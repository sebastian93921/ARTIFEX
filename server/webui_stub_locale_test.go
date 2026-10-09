//go:build !embedui

package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebUIStubUsesRequestLanguage(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		(&Server{}).webuiHandler().ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("status=%d", rec.Code)
		}
		// A removed language negotiates back to the English stub.
		if !strings.Contains(rec.Body.String(), "The frontend is not embedded") {
			t.Fatalf("%s response=%q", lang, rec.Body.String())
		}
	}
}
