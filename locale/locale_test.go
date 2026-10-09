package locale

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]struct {
		want Lang
		ok   bool
	}{
		"en":    {En, true},
		"EN":    {En, true},
		"en-US": {En, true},
		"en_GB": {En, true},
		"ko":    {Default, false},
		"ko-KR": {Default, false},
		"KO":    {Default, false},
		"fr":    {Default, false},
		"":      {Default, false},
		"zh-CN": {Default, false},
	}
	for in, want := range cases {
		got, ok := Normalize(in)
		if got != want.want || ok != want.ok {
			t.Errorf("Normalize(%q) = (%q,%v), want (%q,%v)", in, got, ok, want.want, want.ok)
		}
	}
}

func TestFromRequestPrecedence(t *testing.T) {
	SetServerDefault(En)
	// An unsupported query language falls through to the header.
	r := newReq("/x?lang=ko", "en-US,en;q=0.9", "en")
	if got := FromRequest(r); got != En {
		t.Errorf("unsupported query should fall through: got %q", got)
	}
	// Accept-Language wins over cookie when no query.
	r = newReq("/x", "zh-KR,zh;q=0.9,en;q=0.5", "en")
	if got := FromRequest(r); got != En {
		t.Errorf("accept-language should win over cookie: got %q", got)
	}
	// Unsupported cookie and header leave the server default.
	r = newReq("/x", "fr-FR,fr;q=0.9", "ko")
	if got := FromRequest(r); got != En {
		t.Errorf("server default should apply: got %q", got)
	}
	// Server default when nothing matches.
	r = newReq("/x", "", "")
	if got := FromRequest(r); got != En {
		t.Errorf("default en: got %q", got)
	}
}

func TestAcceptLanguageQWeights(t *testing.T) {
	// en is the only candidate: unsupported higher-q entries are skipped.
	r := newReq("/x", "ko;q=0.3, en;q=0.9", "")
	if got := FromRequest(r); got != En {
		t.Errorf("q-weight pick: got %q", got)
	}
	// unsupported primary with supported fallback
	r = newReq("/x", "fr, en-US;q=0.8", "")
	if got := FromRequest(r); got != En {
		t.Errorf("fallback to en: got %q", got)
	}
}

func TestFromEnvPrefersARTIFEX(t *testing.T) {
	env := map[string]string{"ARTIFEX_LANGUAGE": "en"}
	if l, ok := FromEnv(func(k string) string { return env[k] }); !ok || l != En {
		t.Errorf("ARTIFEX_LANGUAGE should resolve: got %q,%v", l, ok)
	}
	// A removed language no longer resolves.
	env = map[string]string{"ARTIFEX_LANGUAGE": "ko"}
	if _, ok := FromEnv(func(k string) string { return env[k] }); ok {
		t.Errorf("removed language should not resolve")
	}
	if _, ok := FromEnv(func(string) string { return "" }); ok {
		t.Errorf("empty env should not resolve")
	}
	// OS LANG must be ignored
	env = map[string]string{"LANG": "ko_KR.UTF-8"}
	if _, ok := FromEnv(func(k string) string { return env[k] }); ok {
		t.Errorf("OS LANG must not be consulted")
	}
}

func TestContextRoundTrip(t *testing.T) {
	SetServerDefault(En)
	ctx := WithLang(context.Background(), "ko")
	if got := FromContext(ctx); got != En {
		t.Errorf("removed language must coerce to default: got %q", got)
	}
	ctx = WithLang(context.Background(), En)
	if got := FromContext(ctx); got != En {
		t.Errorf("FromContext = %q", got)
	}
	if got := FromContext(context.Background()); got != En {
		t.Errorf("FromContext default = %q", got)
	}
}

func TestCatalogFallback(t *testing.T) {
	Register("test.hello", "Hello %s")
	if got := T(En, "test.hello", "world"); got != "Hello world" {
		t.Errorf("catalog lookup: %q", got)
	}
	if got := T(En, "test.missing.key"); got != "test.missing.key" {
		t.Errorf("unknown key returns key: %q", got)
	}
}

func newReq(target, acceptLang, cookie string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "http://x"+target, nil)
	if acceptLang != "" {
		r.Header.Set("Accept-Language", acceptLang)
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	return r
}

func TestRejectedLanguageWeights(t *testing.T) {
	for _, header := range []string{"en;q=0", "en;q=-1", "en;q=2", "en;q=NaN", "en;q=Inf", "en;q=broken"} {
		if got, ok := fromAcceptLanguage(header); ok {
			t.Errorf("accepted excluded/malformed language %q: %s", header, got)
		}
	}
	if got, ok := fromAcceptLanguage("ko;q=0,en;q=0.5"); !ok || got != En {
		t.Fatal("q=0 language was selected")
	}
}

func TestLocalizedErrorPreservesWrapping(t *testing.T) {
	cause := errors.New("user supplied 原文 %s")
	Register("Operation %s failed: %w", "Operation %s failed: %w")
	err := Errorf("Operation %s failed: %w", "raw-id", cause)
	if !errors.Is(err, cause) {
		t.Fatal("error wrapping lost")
	}
	if got := ErrorMessage(En, err); got != "Operation raw-id failed: user supplied 原文 %s" {
		t.Fatalf("raw cause changed: %q", got)
	}
}

func TestJoinedErrorsPreserveRawCauses(t *testing.T) {
	Register("Missing task", "Missing task")
	raw := errors.New("driver 原文 %s")
	joined := errors.Join(NewError("Missing task"), raw)
	if got := ErrorMessage(En, joined); got != "Missing task\ndriver 原文 %s" {
		t.Fatalf("joined localization altered raw content: %q", got)
	}
	if !errors.Is(joined, raw) {
		t.Fatal("joined cause identity lost")
	}
}
