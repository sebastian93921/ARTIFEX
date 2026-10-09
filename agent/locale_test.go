package agent

import (
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	actool "github.com/Autumn-27/norma/tool"
	"strings"
	"testing"
)

func TestBuiltinPromptLanguageAndInterpolation(t *testing.T) {
	old := PromptOverride
	PromptOverride = nil
	defer func() { PromptOverride = old }()
	en := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: "用户目标 / 원문"}, locale.En)
	if !strings.Contains(en, "You are the planner") {
		t.Fatal("prompt language selection failed")
	}
	for _, text := range []string{en} {
		if !strings.Contains(text, "用户目标 / 원문") || strings.Contains(text, "{{.Goal}}") {
			t.Fatal("goal altered or not interpolated")
		}
	}
}

func TestEditedPromptIsNotTranslated(t *testing.T) {
	old := PromptOverride
	defer func() { PromptOverride = old }()
	const custom = "用户自己写的模板 {{.Goal}}"
	PromptOverride = func(string) (string, bool) { return custom, true }
	got := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: "original"}, locale.En)
	if got != "用户自己写的模板 original" {
		t.Fatalf("edited prompt changed: %q", got)
	}
}

func TestBuiltinPromptCatalogCoverage(t *testing.T) {
	for _, p := range []string{goalsDefaultTmpl, goalsScopeTail, plannerDefaultTmpl, workerDefaultTmpl, mainAgentDefaultTmpl, autoDefaultTmpl, pentestDefaultTmpl, DefaultAssistantPrompt, ReporterDefaultPrompt, RetesterDefaultPrompt} {
		if _, ok := locale.Lookup(locale.En, p); !ok {
			t.Fatalf("prompt missing from catalog: %.60s", p)
		}
	}
}

func TestBuiltinToolMetadataLanguagePreservesEdits(t *testing.T) {
	en := NewToolSet(nil, "", locale.En).nodeDetail()
	original := en.InputSchema()
	// Simulate a stored English default overriding a runtime Korean tool.
	ko := localizeBuiltinTools([]actool.CoreTool{en}, locale.En)[0]
	if ko.Description() != en.Description() {
		t.Fatal("localization changed English metadata")
	}
	if en.Description() != NewToolSet(nil, "", locale.En).nodeDetail().Description() {
		t.Fatal("shared English metadata mutated")
	}
	data, _ := json.Marshal(original)
	var edited map[string]any
	_ = json.Unmarshal(data, &edited)
	props := edited["properties"].(map[string]any)
	props["id"].(map[string]any)["description"] = "用户原文 custom description"
	custom := DecorateTool(en, "用户原文 custom tool", edited)
	localized := localizeBuiltinTools([]actool.CoreTool{custom}, locale.En)[0]
	if localized.Description() != "用户原文 custom tool" || localized.InputSchema()["properties"].(map[string]any)["id"].(map[string]any)["description"] != "用户原文 custom description" {
		t.Fatal("edited metadata changed")
	}
	if localized.Name() != en.Name() {
		t.Fatal("machine tool name changed")
	}
}
