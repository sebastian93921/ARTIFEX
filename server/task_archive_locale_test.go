package server

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

func TestTaskTrancheBilingualValidation(t *testing.T) {
	long := strings.Repeat("x", db.MaxTaskTemplateNameRunes+1)
	tests := []struct {
		name string
		err  error
		en   string
	}{
		{"archive empty", func() error { _, err := normalizeArchiveIDs(nil); return err }(), "archive_ids must not be empty"},
		{"archive id", func() error { _, err := normalizeArchiveIDs([]int64{-42}); return err }(), "Invalid archive ID -42"},
		{"template", validateTaskTemplateRequest(taskTemplateRequest{Name: &long}), "name must be at most 120 characters"},
		{"archive path", validateArchivePath(t.TempDir(), ""), "Archive package path is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("expected validation failure")
			}
			if got := locale.ErrorMessage(locale.En, tt.err); got != tt.en {
				t.Fatalf("English=%q, want %q", got, tt.en)
			}
		})
	}
}

func TestTaskTrancheLocalizedBatchAndRequestErrors(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En} {
		rec := httptest.NewRecorder()
		w := &localeWriter{ResponseWriter: rec, lang: lang}
		req := httptest.NewRequest(http.MethodPost, "/api/task-categories", strings.NewReader(`{"name":""}`))
		if _, ok := decodeTaskCategoryRequest(w, req); ok {
			t.Fatal("empty category name accepted")
		}
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), locale.Text(lang, "Category name must not be empty")) {
			t.Fatalf("%s response=%d %s", lang, rec.Code, rec.Body.String())
		}
	}
}

func TestTaskTrancheResolutionPreservesProfileData(t *testing.T) {
	s := &Server{}
	p := &db.LLMProfile{ID: 7, Name: "Raw profile 원본", Format: "openai", Model: "user-model"}
	got := s.resolutionFromProfileForLanguage(p, "agent_binding", locale.En)
	if got.Name != p.Name || got.Model != p.Model || got.Source != "agent_binding" || got.ProfileID == nil || *got.ProfileID != p.ID || got.Available {
		t.Fatalf("profile data changed: %+v", got)
	}
	if got.Reason != "LLM profile has no API key" {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestTaskTrancheArchiveErrorsPreserveCauses(t *testing.T) {
	cause := errors.New("driver-owned detail / 원본")
	err := locale.Errorf("stage task archive path %s: %w", "user/path 원본", cause)
	joined := errors.Join(err, locale.NewError("task archive checksum mismatch"))
	if !errors.Is(joined, cause) {
		t.Fatal("lost wrapped error identity")
	}
	ko := locale.ErrorMessage(locale.En, joined)
	for _, part := range []string{"stage task archive path", "user/path 원본", cause.Error(), "checksum mismatch"} {
		if !strings.Contains(ko, part) {
			t.Fatalf("missing %q in %q", part, ko)
		}
	}
}

// Source coverage guards against untranslated literal HTTP and typed-error messages
// in this tranche; driver errors, user strings, SQL identifiers, and archive bytes are excluded.
func TestTaskTrancheMessageCatalogCoverage(t *testing.T) {
	names := []string{"archives", "archive_package", "templates", "categories", "assets", "metadata", "resolution"}
	count := 0
	for _, name := range names {
		path := filepath.Join("task_" + name + ".go")
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			index := -1
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if fun.Name == "writeErr" {
					index = 2
				}
			case *ast.SelectorExpr:
				if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "locale" {
					switch fun.Sel.Name {
					case "NewError", "Errorf":
						index = 0
					case "Text":
						index = 1
					}
				}
			}
			if index < 0 || len(call.Args) <= index {
				return true
			}
			lit, ok := call.Args[index].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			_, found := locale.Lookup(locale.En, key)
			if !found {
				t.Errorf("%s: missing message %q", path, key)
			}
			count++
			return true
		})
	}
	if count < 60 {
		t.Fatalf("only %d messages checked", count)
	}
}
