package server

import (
	"context"
	"net/http"
	"os"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

const settingLanguage = "language"

// loadLanguage reads the durable application default. It never consults the OS locale.
func loadLanguage(pg *db.DB) error {
	lang, _ := locale.FromEnv(os.Getenv)
	raw, exists, err := pg.GetSetting(settingLanguage)
	if err != nil {
		return err
	}
	if exists {
		if stored, ok := locale.Normalize(raw); ok {
			lang = stored
		}
	}
	locale.SetServerDefault(lang)
	return nil
}

// localeWriter carries response language without altering JSON payloads or evidence.
// Unwrap preserves ResponseController support; Flush preserves existing SSE writers.
type localeWriter struct {
	http.ResponseWriter
	lang locale.Lang
}

func (w *localeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *localeWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *localeWriter) Language() locale.Lang       { return w.lang }
func responseLanguage(w http.ResponseWriter) locale.Lang {
	if lw, ok := w.(interface{ Language() locale.Lang }); ok {
		return lw.Language()
	}
	return locale.ServerDefault()
}
func withLocale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := locale.FromRequest(r)
		w.Header().Set("Content-Language", string(lang))
		w.Header().Add("Vary", "Accept-Language")
		w.Header().Add("Vary", "Cookie")
		next.ServeHTTP(&localeWriter{w, lang}, r.WithContext(locale.WithLang(r.Context(), lang)))
	})
}

// backgroundLanguage captures a preference at run start, retaining server cancellation.
func backgroundLanguage(parent, request context.Context) context.Context {
	return locale.WithLang(parent, locale.FromContext(request))
}

// taskLanguage persists a new task's output preference in the existing settings
// table. Older tasks without a preference use the configured server default.
func taskLanguage(pg *db.DB, id string) locale.Lang {
	if pg == nil {
		return locale.ServerDefault()
	}
	raw, ok, err := pg.GetSetting("task_language." + id)
	if err == nil && ok {
		if lang, valid := locale.Normalize(raw); valid {
			return lang
		}
	}
	return locale.ServerDefault()
}

// writeError retains structured built-in error templates until the HTTP boundary.
// Driver errors and arbitrary user content are returned without translation.
func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": locale.ErrorMessage(responseLanguage(w), err)})
}

// childTaskLanguage prefers the current run's explicit preference, then its
// parent task, then the durable server default. It does not alter the parent.
func (s *Server) childTaskLanguage(ctx context.Context, parentID string) locale.Lang {
	if lang, ok := locale.Explicit(ctx); ok {
		return lang
	}
	if parentID != "" {
		return taskLanguage(s.m.pg, parentID)
	}
	return locale.ServerDefault()
}

// taskOutputLanguage resolves persisted output preference for generated task messages.
func (s *Server) taskOutputLanguage(id string) locale.Lang {
	if s.m == nil {
		return locale.FromContext(s.ctx)
	}
	return taskLanguage(s.m.pg, id)
}
