package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"net/url"
	"strings"
)

// telegramTextLimit is sendMessage's text limit in characters.
const telegramTextLimit = 4096

// telegramChannel implements the Telegram Bot API. Authentication resides entirely in
// /bot<token>/sendMessage with no signature. HTML mode needs only & < > escaping, unlike MarkdownV2's
// eighteen special characters, any missed escape of which can reject a message. Inspect ok because
// business failures may use HTTP 200.
type telegramChannel struct{}

func (telegramChannel) Kind() string { return KindTelegram }

// Telegram allows about one message/second in direct chats and twenty/minute in groups; use the
// conservative rate.
func (telegramChannel) DefaultRatePerMin() int { return 20 }

// The bot token is a credential; chat_id only identifies a recipient and cannot send without the
// token.
func (telegramChannel) SecretKeys() []string { return []string{"bot_token"} }

// base_url determines the token's API recipient, including custom proxies; changing it requires an
// explicit token choice.
func (telegramChannel) DestinationKeys() []string { return []string{"base_url"} }

func (telegramChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "bot_token") == "" {
		return locale.NewError("Bot token is required")
	}
	if cfgString(cfg, "chat_id") == "" {
		return locale.NewError("Chat ID is required")
	}
	if base := cfgString(cfg, "base_url"); base != "" {
		if err := validateHTTPURL(base); err != nil {
			return locale.Errorf("Invalid API URL: %w", err)
		}
	}
	return nil
}

func (c telegramChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := telegramEndpoint(cfg)
	if err != nil {
		return 0, Permanent(err)
	}
	text, kept := telegramHTML(m)
	payload := map[string]any{
		"chat_id":                  cfgString(cfg, "chat_id"),
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	}
	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, locale.Errorf("Failed to parse Telegram response: %w (%s)", err, snippet(raw))
	}
	if res.OK {
		return kept, nil
	}
	// 429 rate limits can recover with backoff. Other errors (400 arguments, 401 token, 403 blocked bot,
	// 404 missing chat) are configuration failures that retries cannot repair.
	if res.ErrorCode == 429 {
		return 0, locale.Errorf("Telegram rate limit: %s", res.Description)
	}
	return 0, Permanent(locale.Errorf("Telegram returned error %d: %s", res.ErrorCode, res.Description))
}

// telegramEndpoint builds sendMessage URLs. An empty base_url uses the official API; custom values
// support self-hosted Bot API proxies in restricted networks.
func telegramEndpoint(cfg map[string]any) (string, error) {
	base := cfgString(cfg, "base_url")
	if base == "" {
		base = "https://api.telegram.org"
	}
	base = strings.TrimSuffix(base, "/")
	token := cfgString(cfg, "bot_token")
	raw := base + "/bot" + token + "/sendMessage"
	u, err := url.Parse(raw)
	if err != nil {
		// Do not expose parsing errors or the raw address because it contains the bot token.
		return "", locale.Errorf("Failed to build API URL (API base URL: %s)", redactedTargetArgument(base))
	}
	return u.String(), nil
}

// telegramHTML returns the HTML body and actual included count; see Channel.Send.
func telegramHTML(m Message) (string, int) {
	var b strings.Builder
	b.WriteString("<b>" + telegramEscape(markdownTitle(m)) + "</b>\n")
	if m.Batch {
		// Telegram limits characters, so packing uses runeSize.
		footer := ""
		if m.HomeURL != "" {
			footer = fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "\n\n<a href=\"%s\">View all in ARTIFEX</a>"), telegramEscapeAttr(m.HomeURL))
		}
		kept := packItemCount(m.Items, telegramTextLimit, telegramReservedRunes, footer, runeSize, func(it Item, idx int) string {
			return telegramBatchLine(it, idx+1, locale.Resolve(m.Language))
		})
		items := m.Items[:kept]
		b.Reset()
		b.WriteString("<b>" + telegramEscape(telegramBatchTitle(m, items, len(m.Items))) + "</b>")
		for i, it := range items {
			b.WriteString("\n" + telegramEscape(telegramBatchLine(it, i+1, locale.Resolve(m.Language))))
		}
		b.WriteString(footer)
		return TruncateHTML(b.String(), telegramTextLimit), kept
	}
	if len(m.Items) == 0 {
		return b.String(), 0
	}
	it := m.Items[0]
	if it.IsStatusChange() {
		b.WriteString(fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "\n<b>Status change</b>: %s → %s"),
			telegramEscape(StatusLabel(it.FromStatus, locale.Resolve(m.Language))), telegramEscape(StatusLabel(it.ToStatus, locale.Resolve(m.Language)))))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title(locale.Resolve(m.Language)) {
		b.WriteString(locale.Text(locale.Resolve(m.Language), "\n<b>Type</b>: ") + telegramEscape(it.VulnClass))
	}
	if a := assetLine(it.Assets, maxAssetsShown, locale.Resolve(m.Language)); a != "" {
		b.WriteString(locale.Text(locale.Resolve(m.Language), "\n<b>Assets</b>: ") + telegramEscape(a))
	}
	if s := OneLine(it.Summary, maxSummaryRunes); s != "" {
		b.WriteString(locale.Text(locale.Resolve(m.Language), "\n<b>Summary</b>: ") + telegramEscape(s))
	}
	if it.DetailURL != "" {
		b.WriteString(fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "\n\n<a href=\"%s\">View details</a>"), telegramEscapeAttr(it.DetailURL)))
	}
	return TruncateHTML(b.String(), telegramTextLimit), 1
}

// telegramReservedRunes reserves character capacity for the title and continuation notice.
const telegramReservedRunes = 160

// telegramBatchLine returns an unescaped digest entry; its caller applies output escaping.
func telegramBatchLine(it Item, idx int, langs ...locale.Lang) string {
	if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
		return fmt.Sprintf("%d. %s · %s — %s", idx, SeverityLabel(it.Severity, locale.First(langs)), it.Title(locale.First(langs)), a)
	}
	return fmt.Sprintf("%d. %s · %s", idx, SeverityLabel(it.Severity, locale.First(langs)), it.Title(locale.First(langs)))
}

// telegramBatchTitle identifies the total batch and explicitly reports the included and remaining
// counts when only part fits.
func telegramBatchTitle(m Message, items []Item, total int) string {
	title := fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "Finding digest · %d total"), total)
	if extra := total - len(items); extra > 0 {
		title += fmt.Sprintf(locale.Text(locale.Resolve(m.Language), " (showing the first %d; the remaining %d will follow)"), len(items), extra)
	}
	if m.WindowMinutes > 0 {
		title = fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "Last %d minutes · %s"), m.WindowMinutes, title)
	}
	return title
}

// telegramEscape escapes the three HTML text entities Telegram recognizes. Existing entities are
// intentionally escaped again to display raw user text instead of interpreting injected HTML.
func telegramEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// telegramEscapeAttr also escapes quotes to prevent URLs from closing href and creating an injection
// point.
func telegramEscapeAttr(s string) string {
	s = telegramEscape(s)
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
