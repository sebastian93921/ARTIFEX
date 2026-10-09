package db

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/locale"
)

func literalTemplate(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, err := strconv.Unquote(v.Value)
			return s, err == nil
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			a, ok := literalTemplate(v.X)
			b, ok2 := literalTemplate(v.Y)
			return a + b, ok && ok2
		}
	}
	return "", false
}

// This checks all explicit DB error templates, including concatenated literals,
// so a new error cannot silently fall back to English in Korean requests.
func TestDatabaseErrorTemplateCoverage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if helper, ok := call.Fun.(*ast.Ident); ok && helper.Name == "newCompanyScopeValidationError" {
				template, literal := literalTemplate(call.Args[0])
				_, present := locale.Lookup(locale.En, template)
				if !literal || !present {
					t.Errorf("%s: incomplete validation template %q", path, template)
				}
				count++
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "locale" || (sel.Sel.Name != "Errorf" && sel.Sel.Name != "NewError") {
				return true
			}
			template, ok := literalTemplate(call.Args[0])
			if !ok {
				if name, ok := call.Args[0].(*ast.Ident); ok && path == "companies.go" && name.Name == "template" {
					return true
				}
				t.Errorf("%s: error template is not a literal", path)
				return true
			}
			en, present := locale.Lookup(locale.En, template)
			if !present || en != template {
				t.Errorf("%s: incomplete bilingual error template %q", path, template)
			}
			count++
			return true
		})
	}
	if count < 100 {
		t.Fatalf("only %d DB error templates checked", count)
	}
}

func TestDatabaseTypedErrorsPreserveEvidenceAndIdentity(t *testing.T) {
	raw := "user-host.example/원본"
	err := ValidateAssetIP(raw)
	if !errors.Is(err, ErrAssetIPInvalid) {
		t.Fatalf("lost error identity: %v", err)
	}
	en := locale.ErrorMessage(locale.En, err)
	if !strings.Contains(en, "IPv4/IPv6") || !strings.Contains(en, raw) {
		t.Fatalf("localized error lost template or raw input: %q", en)
	}
	external := errors.New("driver-owned detail / 원본")
	wrapped := locale.Errorf("restore %s: %w", "user-table", external)
	if !errors.Is(wrapped, external) || !strings.Contains(locale.ErrorMessage(locale.En, wrapped), external.Error()) {
		t.Fatal("wrapped external cause changed")
	}
	if locale.ErrorMessage(locale.En, external) != external.Error() {
		t.Fatal("unknown error translated")
	}
}

func TestBuiltinMetadataLocalePreservesCustomValues(t *testing.T) {
	for _, seed := range builtinAgents {
		for _, lang := range []locale.Lang{locale.En} {
			a := &Agent{Key: seed.key, Builtin: true, Name: legacyDefaultMetadata(seed.name), Description: legacyDefaultMetadata(seed.desc)}
			LocalizeBuiltinAgentMetadata(a, lang)
			if a.Name != locale.Text(lang, seed.name) || a.Description != locale.Text(lang, seed.desc) {
				t.Fatalf("stock metadata not localized for %s/%s", seed.key, lang)
			}
			a.Name = "Custom name"
			a.Description = "Custom description 원본"
			LocalizeBuiltinAgentMetadata(a, lang)
			if a.Name != "Custom name" || a.Description != "Custom description 원본" {
				t.Fatal("custom metadata changed")
			}
		}
	}
	vars := []PromptVar{{Name: "Goal", Description: "Custom description", Example: "Custom example"}}
	LocalizeBuiltinPromptVars("planner", vars, locale.En)
	if vars[0].Description != "Custom description" || vars[0].Example != "Custom example" {
		t.Fatal("custom variable metadata changed")
	}
}

func TestRetestInitialMessageLanguagePreservesNotes(t *testing.T) {
	r := &FindingRetest{FindingID: 42, Notes: "Raw user evidence / 원본"}
	en := r.InitialMessageForLanguage(locale.En)
	if !strings.HasPrefix(en, "Please retest finding #42.") {
		t.Fatalf("unexpected retest messages: %q", en)
	}
	for _, msg := range []string{en} {
		if !strings.HasSuffix(msg, r.Notes) || !strings.Contains(msg, "get_finding_retest_context") || !strings.Contains(msg, "record_finding_retest_result") {
			t.Fatal("notes or tool contract changed")
		}
	}
}

func TestSeedLocalizationPreservesCustomization(t *testing.T) {
	// This integration test uses only the explicitly supplied test database.
	if os.Getenv("ARTEX_PG_DSN") == "" {
		t.Skip("explicit isolated database required")
	}
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a, err := d.GetAgentByKey("planner")
	if err != nil {
		t.Fatal(err)
	}
	vars, err := d.PromptVars(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := d.Exec(`UPDATE agents SET name=$1,description=$2 WHERE id=$3`, a.Name, a.Description, a.ID); err != nil {
			t.Errorf("restore agent fixture: %v", err)
		}
		for _, v := range vars {
			if _, err := d.Exec(`UPDATE agent_prompt_vars SET description=$1,example=$2 WHERE agent_id=$3 AND var_name=$4`, v.Description, v.Example, a.ID, v.Name); err != nil {
				t.Errorf("restore variable fixture: %v", err)
			}
		}
	}()
	if _, err = d.Exec(`UPDATE agents SET name='Custom seed name',description='Custom seed description' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec(`UPDATE agent_prompt_vars SET description='Custom variable',example='Custom example' WHERE agent_id=$1 AND var_name='Goal'`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.seedBuiltins(); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetAgentByKey("planner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Custom seed name" || got.Description != "Custom seed description" {
		t.Fatal("localized seed overwrote custom agent metadata")
	}
	gotVars, err := d.PromptVars(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range gotVars {
		if v.Name == "Goal" && (v.Description != "Custom variable" || v.Example != "Custom example") {
			t.Fatal("localized seed overwrote custom variable metadata")
		}
	}
	// Only exact legacy stock defaults are upgraded to canonical English.
	seed := builtinAgents[1]
	if _, err = d.Exec(`UPDATE agents SET name=$1,description=$2 WHERE id=$3`, legacyDefaultMetadata(seed.name), legacyDefaultMetadata(seed.desc), a.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.seedBuiltins(); err != nil {
		t.Fatal(err)
	}
	got, err = d.GetAgentByKey("planner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != seed.name || got.Description != seed.desc {
		t.Fatal("legacy stock metadata was not upgraded")
	}
}

func TestAssetGateLocalePreservesDecisionAndUserNote(t *testing.T) {
	rule := AssetInterceptRule{Kind: "exact_domain", Pattern: "target.example", Note: "Custom note / 원본", Enabled: true}
	en := EvaluateAssetGateForLanguage(locale.En, []AssetInterceptRule{rule}, nil, []string{"target.example"}, nil, nil)
	en2 := EvaluateAssetGateForLanguage(locale.En, []AssetInterceptRule{rule}, nil, []string{"target.example"}, nil, nil)
	if en.Allowed || en2.Allowed || !strings.Contains(en2.Reason, rule.Note) || !strings.Contains(en2.Reason, rule.Pattern) {
		t.Fatalf("gate changed decision/data: %+v / %+v", en, en2)
	}
	miss := EvaluateAssetGateForLanguage(locale.En, nil, []AssetInterceptRule{rule}, []string{"other.example"}, nil, nil)
	if miss.Allowed {
		t.Fatalf("allowlist miss: %+v", miss)
	}
}

func TestCompanyScopeValidationLocale(t *testing.T) {
	err := ValidateCompanyScopeInputBounds([]ScopeInput{{Value: strings.Repeat("x", MaxCompanyScopeRawRunes+1)}})
	var scopeErr *CompanyScopeValidationError
	if !errors.As(err, &scopeErr) || !strings.Contains(locale.ErrorMessage(locale.En, err), "Company scope rule") {
		t.Fatalf("validation error not localized: %v", err)
	}
}

func TestTaskOriginUsesExplicitLanguage(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("Raw description 원본", "Raw goal", TaskCreateOptions{Language: locale.En})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Errorf("delete task fixture: %v", err)
		}
	}()
	var summary string
	if err = d.QueryRow(`SELECT payload->>'summary' FROM exploration_nodes WHERE exploration_id=$1 AND state='origin'`, task.ExplorationID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "작업 시작점: Raw description 원본; 목표: Raw goal" {
		t.Fatalf("origin message=%q", summary)
	}
}
