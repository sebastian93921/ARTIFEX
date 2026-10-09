package server

import (
	"encoding/json"
	"github.com/Autumn-27/artex/locale"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocaleMiddlewareErrorAndSSE(t *testing.T) {
	locale.Register("language must be en", "language must be en")
	h := withLocale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locale.FromContext(r.Context()) != locale.En {
			t.Error("request language missing")
		}
		if _, ok := w.(http.Flusher); !ok {
			t.Error("SSE flushing unavailable")
		}
		writeErr(w, 400, "language must be en")
	}))
	// A removed language (?lang=ko) falls back to English negotiation.
	r := httptest.NewRequest("GET", "/api/settings?lang=ko", nil)
	r.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "language must be en") || w.Header().Get("Content-Language") != "en" {
		t.Fatalf("bad localized response: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Cookie") {
		t.Fatal("cookie language missing cache variation")
	}
}

func TestLocaleMiddlewarePreservesUnknownUserError(t *testing.T) {
	w := httptest.NewRecorder()
	lw := &localeWriter{w, locale.En}
	writeErr(lw, 400, "用户提供的数据 원문")
	if !strings.Contains(w.Body.String(), "用户提供的数据 원문") {
		t.Fatal("unknown content changed")
	}
}

func TestDynamicErrorLocalizesBeforeInterpolation(t *testing.T) {
	const template = "Asset %s is outside the authorized scope (task %d)"
	locale.Register(template, template)
	raw := "用户原文/한국어/%s?q=<raw>"
	err := locale.Errorf(template, raw, 17)
	h := withLocale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeError(w, 400, err) }))
	r := httptest.NewRequest("GET", "/api/test", nil)
	r.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var response struct {
		Error string `json:"error"`
	}
	if decodeErr := json.Unmarshal(w.Body.Bytes(), &response); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if response.Error != "Asset "+raw+" is outside the authorized scope (task 17)" {
		t.Fatalf("wrong localized error or modified raw argument: %q", response.Error)
	}
	if err.Error() != "Asset "+raw+" is outside the authorized scope (task 17)" {
		t.Fatal("error identity changed with request language")
	}
}

func TestUpdateProgressRendersPerSubscriber(t *testing.T) {
	p := updateProgress{message: locale.M("Downloading %s (%s)…", "raw-한글.zip", "12 MB"), cause: locale.Errorf("Release package does not contain %s", "raw-한글.exe")}
	en := p.inLanguage(locale.En)
	if en.Message != "Downloading raw-한글.zip (12 MB)…" {
		t.Fatalf("progress locale mismatch: %q", en.Message)
	}
	if !strings.Contains(en.Error, "Release package") || !strings.Contains(en.Error, "raw-한글.exe") {
		t.Fatalf("error locale/raw argument mismatch: %q", en.Error)
	}
	if p.Message != "" || p.Error != "" {
		t.Fatal("subscriber mutated shared event")
	}
}
