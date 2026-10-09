package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/locale"
)

func TestAssetInterceptLocalizedValidationPreservesRules(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En} {
		req := assetInterceptRuleReq{Kind: "exact_ip", Pattern: "raw-invalid-address", Note: "User note"}
		err := validateAssetInterceptRuleReq(&req)
		if err == nil {
			t.Fatal("Invalid IP accepted")
		}
		text := locale.ErrorMessage(lang, err)
		if !strings.Contains(text, req.Pattern) {
			t.Fatalf("Raw pattern lost: %q", text)
		}
		label := "not a valid IP address"
		if !strings.Contains(text, label) {
			t.Fatalf("%s validation not localized: %q", lang, text)
		}
		taskReq := taskInterceptRuleReq{Kind: "fuzzy_domain", Pattern: "  custom.example  ", Note: "Raw note"}
		if err := validateTaskInterceptRuleReq(&taskReq); err != nil {
			t.Fatal(err)
		}
		if taskReq.Action != "block" || taskReq.Pattern != "custom.example" || taskReq.Note != "Raw note" {
			t.Fatalf("Rule semantics changed: %+v", taskReq)
		}
		taskReq.Action = "invalid"
		if err := validateTaskInterceptRuleReq(&taskReq); err == nil {
			t.Fatal("Invalid action accepted")
		}
		filter, err := interceptFilterParams(url.Values{"status": {"invalid"}})
		if err == nil || filter.Status != "invalid" {
			t.Fatal("Invalid filter handling changed")
		}

	}
}

func TestAssetInterceptRequestLanguageErrors(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En} {
		t.Run(string(lang), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/intercept/1/decide", strings.NewReader(`{"decision":"invalid"}`))
			r.SetPathValue("id", "1")
			r.Header.Set("Accept-Language", string(lang))
			w := httptest.NewRecorder()
			withLocale(http.HandlerFunc((&Server{}).interceptDecide)).ServeHTTP(w, r)
			label := "decision must be allowed or denied"
			if w.Code != 400 || !strings.Contains(w.Body.String(), label) {
				t.Fatalf("Unexpected decision response: %d %s", w.Code, w.Body)
			}
			r = httptest.NewRequest(http.MethodPost, "/api/companies", strings.NewReader(`{"scope":[123]}`))
			r.Header.Set("Accept-Language", string(lang))
			w = httptest.NewRecorder()
			withLocale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var value struct {
					Scope companyScopeInputs `json:"scope"`
				}
				decodeCompanyMutationRequest(w, r, &value)
			})).ServeHTTP(w, r)
			label = "scope[0] must be a string"
			if w.Code != 400 || !strings.Contains(w.Body.String(), label) {
				t.Fatalf("Nested validation lost language: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestAssetInterceptCatalogCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, path := range []string{"assets.go", "asset_intercept.go", "task_intercept.go", "intercept.go"} {
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
			lit, ok := call.Args[index].(*ast.BasicLit)
			if !ok {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			seen[key] = true
			en, ok := locale.Lookup(locale.En, key)
			if !ok || en != key {
				t.Errorf("Missing English template %q", key)
			}
			return true
		})
	}
	t.Logf("Verified %d asset/interception templates", len(seen))
}
