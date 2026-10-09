package server

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

func TestOperationsMCPValidationLanguages(t *testing.T) {
	for _, transport := range []string{"stdio", "http", "sse", "custom%raw"} {
		_, err := connectMCP(context.Background(), &db.MCPServer{Transport: transport})
		if err == nil {
			t.Fatalf("%s accepted without configuration", transport)
		}
		en := locale.ErrorMessage(locale.En, err)
		if transport == "custom%raw" && !strings.Contains(en, transport) {
			t.Fatalf("raw transport changed: %q", en)
		}
	}
}

func TestOperationsCustomToolLocalePreservesSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","description":"User description 원본","properties":{"args":{"type":"string","description":"User args 100%"}}}`)
	got := ensureSchemaForLanguage(raw, locale.En)
	if got["description"] != "User description 원본" {
		t.Fatal("user schema translated")
	}
	props := got["properties"].(map[string]any)
	if props["args"].(map[string]any)["description"] != "User args 100%" {
		t.Fatal("user parameter description changed")
	}
	fallback := ensureSchemaForLanguage(nil, locale.En)["properties"].(map[string]any)["args"].(map[string]any)
	if fallback["description"] != "Command/arguments (free text)" {
		t.Fatalf("fallback=%v", fallback)
	}
	s := &Server{}
	ctx := locale.WithLang(context.Background(), locale.En)
	result, err := s.runCommandTool(ctx, json.RawMessage(`{}`), nil, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Flatten(), "command is empty") {
		t.Fatalf("command result=%+v err=%v", result, err)
	}
	result, err = s.runHTTPTool(ctx, json.RawMessage(`{}`), nil, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Flatten(), "HTTP URL is empty") {
		t.Fatalf("HTTP result=%+v err=%v", result, err)
	}
}

func TestOperationsAttachmentAndWorkspaceDataRemainVerbatim(t *testing.T) {
	dir := t.TempDir()
	msg := "User text 100% 원본"
	name := "attachment.txt"
	got := composeAgentMessageForLanguage(msg, []chatAttachment{{Path: name, Size: 12}}, dir, locale.En)
	if !strings.HasPrefix(got, msg) || !strings.Contains(got, "[User-uploaded attachments]") || !strings.Contains(got, filepath.Join(dir, name)) {
		t.Fatalf("attachment message=%q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: &Manager{dir: dir}}
	rec := httptest.NewRecorder()
	s.wsRead(&localeWriter{rec, locale.En}, httptest.NewRequest(http.MethodGet, "/api/workspace/read?path="+name, nil))
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || payload.Content != msg {
		t.Fatalf("workspace content changed: %d %+v", rec.Code, payload)
	}
	rec = httptest.NewRecorder()
	s.wsRead(&localeWriter{rec, locale.En}, httptest.NewRequest(http.MethodGet, "/api/workspace/read?path=missing", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "File not found") {
		t.Fatalf("workspace error=%d %s", rec.Code, rec.Body.String())
	}
}

func TestOperationsSyncParseErrorsUseRequestLanguage(t *testing.T) {
	for _, kind := range []string{"subdomain", "app", "service"} {
		got := (&Server{}).ssIngestForLanguage(nil, kind, json.RawMessage(`{`), map[string]int{}, locale.En)
		if !strings.Contains(got, "Could not parse") || !strings.Contains(got, "unexpected end of JSON input") {
			t.Fatalf("%s lost localized prefix/raw decoder error: %q", kind, got)
		}
	}
}

func TestOperationsCustomToolValidationHTTP(t *testing.T) {
	dsn := os.Getenv("ARTIFEX_PG_DSN")
	if dsn == "" {
		t.Skip("explicit isolated database required")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	s := &Server{m: &Manager{pg: pg}}
	for _, lang := range []locale.Lang{locale.En} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/tools/custom", strings.NewReader(`{"key":"INVALID","kind":"command"}`))
		s.pgCreateCustomTool(&localeWriter{rec, lang}, req)
		want := locale.Text(lang, "key must start with a lowercase letter and contain only lowercase letters, digits, or underscores")
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s response=%d %s", lang, rec.Code, rec.Body.String())
		}
	}
}

func TestOperationsMessageCatalogCoverage(t *testing.T) {
	count := 0
	for _, name := range []string{"customtool", "sync_scopesentry", "workspace", "chatupload", "commands", "llmpool", "llmretry", "mcpdiscover"} {
		f, err := parser.ParseFile(token.NewFileSet(), name+".go", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			index := -1
			switch fn := c.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "writeErr" {
					index = 2
				}
			case *ast.SelectorExpr:
				if pkg, ok := fn.X.(*ast.Ident); ok && pkg.Name == "locale" {
					switch fn.Sel.Name {
					case "Text":
						index = 1
					case "Errorf", "NewError":
						index = 0
					}
				}
			}
			if index < 0 || len(c.Args) <= index {
				return true
			}
			lit, ok := c.Args[index].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			_, found := locale.Lookup(locale.En, key)
			if !found {
				t.Errorf("%s: missing message %q", name, key)
			}
			count++
			return true
		})
	}
	if count < 50 {
		t.Fatalf("only %d message calls checked", count)
	}
}
