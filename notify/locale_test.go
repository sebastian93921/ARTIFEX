package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sebastian93921/artifex/locale"
)

func localizedMessage(lang locale.Lang) Message {
	return Message{Language: lang, HomeURL: "https://example.test/dashboard", Items: []Item{{
		FindingID: 7, Name: "Raw user title", VulnClass: "custom-class", Severity: "high",
		Summary: "Raw evidence stays unchanged", Assets: []string{"raw.example.test"},
		DetailURL: "https://example.test/finding/7", FromStatus: "pending", ToStatus: "fixed",
	}}}
}

func TestNotificationRenderLanguagesPreserveRawContent(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En} {
		t.Run(string(lang), func(t *testing.T) {
			m := localizedMessage(lang)
			md, kept := markdownBody(m, 0)
			if kept != 1 {
				t.Fatalf("Markdown kept=%d, want 1", kept)
			}
			tg, kept := telegramHTML(m)
			if kept != 1 {
				t.Fatalf("Telegram kept=%d, want 1", kept)
			}
			card, kept := feishuCard(m)
			if kept != 1 {
				t.Fatalf("Feishu kept=%d, want 1", kept)
			}
			encoded, err := json.Marshal(card)
			if err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"markdown": md, "telegram": tg, "email": htmlBody(m, 0), "feishu": string(encoded)} {
				for _, raw := range []string{m.Items[0].Name, m.Items[0].Summary, m.Items[0].VulnClass, m.Items[0].Assets[0]} {
					if !strings.Contains(body, raw) {
						t.Errorf("%s/%s lost raw value %q", lang, name, raw)
					}
				}
				expected := "Status change"
				if !strings.Contains(body, expected) {
					t.Errorf("%s/%s missing localized status label: %s", lang, name, body)
				}
			}
			d := newWebhookTemplateData(m)
			if d.Items[0].Name != m.Items[0].Name || d.Items[0].Summary != m.Items[0].Summary || d.Items[0].Severity != "high" || d.Items[0].FromStatus != "pending" {
				t.Fatalf("Webhook data changed user content or machine IDs: %+v", d.Items[0])
			}
			if d.Items[0].SeverityLabel != SeverityLabel("high", lang) {
				t.Fatal("Webhook label ignored language")
			}
			// User-authored template text must not be translated even when it matches a built-in label.
			body, err := renderWebhookBody(`{"raw":"View details", "name":{{json (index .Items 0).Name}}}`, m)
			if err != nil || !strings.Contains(body, `"raw":"View details"`) {
				t.Fatalf("User template was changed: %s, %v", body, err)
			}
		})
	}
}

func TestNotificationLanguagePrecedenceAndConcurrentIsolation(t *testing.T) {
	old := locale.ServerDefault()
	defer locale.SetServerDefault(old)
	locale.SetServerDefault(locale.En)
	t.Setenv("LANG", "ko_KR.UTF-8")
	t.Setenv("LC_ALL", "ko_KR.UTF-8")
	if got := SeverityLabel("high"); got != "🟠 High" {
		t.Fatalf("OS locale changed default: %q", got)
	}
	locale.SetServerDefault("ko")
	if got := (Message{}).withContextLanguage(context.Background()).Language; got != locale.En {
		t.Fatalf("Server default not applied: %s", got)
	}
	m := localizedMessage(locale.En)
	if got := m.withContextLanguage(context.Background()).Language; got != locale.En {
		t.Fatalf("Message language lost: %s", got)
	}
	if got := m.withContextLanguage(locale.WithLang(context.Background(), "ko")).Language; got != locale.En {
		t.Fatalf("Context did not take precedence: %s", got)
	}
	var wg sync.WaitGroup
	for _, lang := range []locale.Lang{locale.En} {
		wg.Add(1)
		go func(lang locale.Lang) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				m := localizedMessage(lang)
				body, _ := markdownBody(m, 0)
				label := "**Status change**"
				if !strings.Contains(body, label) {
					t.Errorf("Concurrent locale crossed: %s", body)
					return
				}
			}
		}(lang)
	}
	wg.Wait()
}

func TestNotificationErrorsLocalizeOnlyBuiltins(t *testing.T) {
	fallback := locale.Errorf("Request failed: %s", redactedTransportArgument(&url.Error{Op: "POST", URL: "https://example.test/secret"}))
	if got := ErrorMessage(locale.En, fallback); !strings.Contains(got, "unknown error") || strings.Contains(got, "secret") {
		t.Fatalf("Fallback error language/redaction mismatch: %s", got)
	}
	invalid := locale.Errorf("Cannot parse URL (%s)", redactedTargetArgument(":%broken"))
	if got := ErrorMessage(locale.En, invalid); !strings.Contains(got, "Cannot parse URL") {
		t.Fatalf("Invalid URL fallback not localized: %s", got)
	}
	raw := errors.New("Raw provider response: View details")
	err := Permanent(locale.Errorf("Failed to parse Telegram response: %w (%s)", raw, "raw snippet"))
	got := ErrorMessage(locale.En, err)
	if !strings.Contains(got, "Failed to parse Telegram response") || !strings.Contains(got, raw.Error()) || !strings.Contains(got, "raw snippet") {
		t.Fatalf("Incorrect localized error: %s", got)
	}
	if !IsPermanent(err) || !errors.Is(err, raw) {
		t.Fatal("Error classification or wrapping changed")
	}
	if got := ErrorMessage(locale.En, raw); got != raw.Error() {
		t.Fatalf("Unknown error changed: %q", got)
	}
	changed := &ErrDestinationChangedWithoutCredentials{Changed: []string{"base_url"}, Missing: []string{"bot_token"}}
	if got := ErrorMessage(locale.En, changed); !strings.Contains(got, "changed") || !strings.Contains(got, "bot_token") {
		t.Fatalf("Destination error not localized: %s", got)
	}
	for _, reply := range []string{"550 rejected", "450 greylisted"} {
		e := smtpEnvelopeError("Recipient %s was rejected: %w", "raw@example.test", errors.New(reply))
		if IsPermanent(e) != (reply[:1] == "5") {
			t.Fatalf("SMTP classification changed: %v", e)
		}
		if got := ErrorMessage(locale.En, e); !strings.Contains(got, "Recipient raw@example.test") || !strings.Contains(got, reply) {
			t.Fatalf("SMTP error lost localization/raw data: %s", got)
		}
	}
}

// Audit every explicit built-in call against both catalog languages. Structural
// formatting alone does not need translation; all human templates must be registered.
func TestNotificationCatalogCoverage(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "locale" {
				return true
			}
			index := 0
			switch sel.Sel.Name {
			case "Text":
				index = 1
			case "Errorf", "NewError":
			default:
				return true
			}
			if len(call.Args) <= index {
				return true
			}
			lit, ok := call.Args[index].(*ast.BasicLit)
			if !ok {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if key == "%s: %w" {
				return true
			}
			seen[key] = true
			en, ok := locale.Lookup(locale.En, key)
			if !ok || en != key {
				t.Errorf("Missing English template %q", key)
			}
			return true
		})
	}
	if len(seen) < 90 {
		t.Fatalf("Unexpectedly few audited templates: %d", len(seen))
	}
	t.Logf("Verified %d explicit notification templates", len(seen))
}

func TestNotificationDigestPackingInBothLanguages(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En} {
		m := batchMsg(200)
		m.Language = lang
		md, kept := markdownBody(m, weComMarkdownLimit)
		if kept <= 0 || kept >= len(m.Items) || len(md) > weComMarkdownLimit {
			t.Fatalf("%s invalid Markdown packing: kept=%d bytes=%d", lang, kept, len(md))
		}
		// Verify the last claimed item is actually present, not silently lost to a longer translated header.
		last := fmt.Sprintf("%d. **", kept)
		if !strings.Contains(md, last) {
			t.Fatalf("%s receipt claims missing final item %d", lang, kept)
		}
		tg, kept := telegramHTML(m)
		if kept <= 0 || kept >= len(m.Items) || len([]rune(tg)) > telegramTextLimit {
			t.Fatalf("%s invalid Telegram packing", lang)
		}
		if !strings.Contains(tg, fmt.Sprintf("\n%d. ", kept)) {
			t.Fatalf("%s Telegram receipt claims missing item %d", lang, kept)
		}
	}
}

func TestNotificationSendUsesContextLanguage(t *testing.T) {
	for _, kind := range []string{KindDingTalk, KindFeishu, KindWeCom, KindTelegram, KindWebhook} {
		t.Run(kind, func(t *testing.T) {
			srv := capturePost(t, `{"ok":true,"errcode":0,"code":0}`, func(t *testing.T, body map[string]any, r *http.Request) {
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Error(err)
					return
				}
				if !strings.Contains(string(encoded), "High") {
					t.Errorf("%s Send ignored context language: %s", kind, encoded)
				}
				if !strings.Contains(string(encoded), "Raw user title") {
					t.Errorf("%s Send changed user title", kind)
				}
			})
			addr := srv.URL
			cfg := map[string]any{"webhook": addr, "url": addr, "base_url": addr, "bot_token": "synthetic-token", "chat_id": "1"}
			channel, _ := Get(kind)
			_, err := channel.Send(locale.WithLang(context.Background(), locale.En), cfg, localizedMessage(locale.En))
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNotificationEmailSendUsesContextLanguage(t *testing.T) {
	f := newFakeSMTP(t)
	kept, err := (emailChannel{}).Send(locale.WithLang(context.Background(), locale.En), emailCfg(t, f, nil), localizedMessage(locale.En))
	if err != nil || kept != 1 {
		t.Fatalf("SMTP send: kept=%d error=%v", kept, err)
	}
	raw := f.body()
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("Missing email body separator: %q", raw)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "Status change") || !strings.Contains(string(decoded), "Raw evidence stays unchanged") {
		t.Fatalf("Email language/raw evidence mismatch: %s", decoded)
	}
	for _, line := range strings.Split(parts[0], "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			subject, err := new(mime.WordDecoder).DecodeHeader(strings.TrimPrefix(line, "Subject: "))
			if err != nil || !strings.Contains(subject, "High") || !strings.Contains(subject, "Raw user title") {
				t.Fatalf("Email subject mismatch: %q, %v", subject, err)
			}
			return
		}
	}
	t.Fatal("Missing Subject header")
}
