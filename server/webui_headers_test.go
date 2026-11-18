package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleBrowserProtections(t *testing.T) {
	handler := (&Server{}).Handler()
	headers := map[string]string{
		"Content-Security-Policy": "frame-ancestors 'self'",
		"X-Frame-Options":         "SAMEORIGIN",
		"Referrer-Policy":         "same-origin",
	}

	for _, path := range []string{"/", "/settings/"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		sent := res.Result().Header
		for name, want := range headers {
			if got := sent.Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", path, name, got, want)
			}
		}
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("health: status = %d, want %d", res.Code, http.StatusOK)
	}
	sent := res.Result().Header
	for name := range headers {
		if got := sent.Get(name); got != "" {
			t.Errorf("health: unexpected %s = %q", name, got)
		}
	}
}
