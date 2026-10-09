package agent

import (
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"

	"github.com/sebastian93921/artifex/db"
	actool "github.com/Autumn-27/norma/tool"
)

const findingIDGuidance = "\n\n**Finding ID contract**: finding_id identifies the independent finding record; finding_node_id identifies the exploration node. The id in list_findings/list_task_findings/node_detail/get_task_node_detail remains the exploration node ID; read the independent ID from finding_id in that same result. get_finding_traffic/bind_finding_traffic use the independent finding_id. The legacy update_finding_report finding_id argument still takes finding_node_id. Do not use the number on report_finding's first line for evidence tools, or guess another number after an ID error."

// The server supplies the persisted setting. A missing setting/host is off.
// Consulted at assembly and again on writes so an already-running session
// cannot keep binding after the user switches the feature off.
var FindingTrafficBindingEnabled func() bool

func findingTrafficBindingEnabled() bool {
	return FindingTrafficBindingEnabled != nil && FindingTrafficBindingEnabled()
}

// Applied after ToolResolve: user descriptions and prompts remain intact, while
// all actual reporters (including Planner and custom chat agents) see the same
// API contract. Disabled/unbound tools are never reintroduced here.
func findingWorkflowTools(agentKey string, tools []actool.CoreTool, langs ...locale.Lang) ([]actool.CoreTool, string) {
	if !findingTrafficBindingEnabled() {
		out := make([]actool.CoreTool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name() == "bind_finding_traffic" {
				continue
			}
			if agentKey == "reporter" && (tool.Name() == "traffic_search" || tool.Name() == "traffic_get" || tool.Name() == "traffic_blob") {
				continue
			}
			switch tool.Name() {
			case "report_finding", "add_hint", "add_task_hint":
				// Work on a copy: toggling back on must restore the original schema.
				raw, _ := json.Marshal(tool.InputSchema())
				var schema map[string]any
				if json.Unmarshal(raw, &schema) == nil {
					stripTrafficParameters(schema)
					tool = DecorateTool(tool, tool.Description(), schema)
				}
			}
			out = append(out, tool)
		}
		return out, ""
	}
	out := append([]actool.CoreTool(nil), tools...)
	has := map[string]bool{}
	for i, tool := range out {
		has[tool.Name()] = true
		note := ""
		switch tool.Name() {
		case "report_finding":
			note = locale.Text(locale.First(langs), "\nBy default the reporter verifies and binds traffic before writing the report. Preserve validation commands, key output, existing real traffic IDs, and their roles in evidence so the reporter can check execution records; no extra packet lookup is required just for binding. Explicit immediate binding remains supported: traffic_refs or evidence_hint_id may supply verified references; the latter reads structured references from this task's specified hint. Any invalid reference fails the entire submission. TCP/no-packet findings do not need these optional parameters. Returned finding_id and finding_node_id identify the independent record and exploration node respectively.")
		case "add_hint", "add_task_hint":
			note = locale.Text(locale.First(langs), "\nWhen handing off a confirmed finding, retain verified traffic IDs, roles, notes, and order in the hint's traffic_refs (top-level for one hint, in each hints element for batches), and explain the specific vulnerability it proves in text. Do not discard existing traffic references and hand off text alone. Unverified candidates cannot be passed as evidence.")
		case "get_finding_traffic", "bind_finding_traffic", "list_findings", "list_task_findings", "node_detail", "get_task_node_detail", "update_finding_report":
			note = locale.Text(locale.First(langs), findingIDGuidance)
		}
		if note != "" {
			out[i] = DecorateTool(tool, tool.Description()+note, tool.InputSchema())
		}
	}
	guidance := ""
	if has["report_finding"] || has["add_task_hint"] || has["add_hint"] {
		guidance = locale.Text(locale.First(langs), "\n\n**Traffic evidence handoff (optional)**: By default, the reporter binds evidence after finding registration and before report writing. Preserve validation commands, key output, existing real traffic IDs and roles in evidence, including intent_id in tasks for traceability. Do not look up extra packets just for binding. Auto/Planner must preserve references already held by the executor. add_hint/add_task_hint support traffic_refs handoff; report_finding still supports explicit immediate binding through traffic_refs/evidence_hint_id. Register TCP or no-packet findings normally. Never guess IDs or repeat probes only to capture packets.")
		if has["add_task_hint"] && !has["add_hint"] {
			guidance += locale.Text(locale.First(langs), "\nWithout task context in a platform conversation, do not call report_finding directly. Hand off to the existing relevant task with add_task_hint, let its agent register the finding, and verify through list_task_findings.")
		}
		if has["prove_goal"] || has["goal_met"] {
			guidance += locale.Text(locale.First(langs), "\nFinish reporting/handing off existing evidence before declaring the objective complete. Do not end the task or cancel workers merely because a textual finding was registered while evidence handoff remains incomplete. Missing packets do not require waiting or forced capture.")
		}
	}
	if has["update_finding_report"] && has["bind_finding_traffic"] && has["get_finding_traffic"] {
		guidance += locale.Text(locale.First(langs), "\n\n**Automatic traffic association before reporting (enabled)**: Verify and bind traffic for this triggered finding, then write its report. Obtain explicit finding_id and finding_node_id from report_finding JSON or get_task_node_detail/list_task_findings. Read finding details, the intent's execution trace, and existing evidence; prefer real IDs handed off by the reporter. For HTTP validation with traffic tools available, filter candidates with traffic_search and verify each request/response with traffic_get. Domain/time filters do not establish ownership. Bind verified evidence in reproduction order using bind_finding_traffic(finding_id, traffic_refs), selecting baseline/proof/verification/supporting and explaining each role. Operate only on this finding; do not create duplicates or probe the target again. After binding, call get_finding_traffic again for the latest version, read the needed bodies, and pass that actually read version as evidence_version to update_finding_report (whose finding_id argument still takes finding_node_id). Do not duplicate existing bindings. For TCP, uncaptured traffic, unavailable tools, or no exact match, skip binding and write the report from textual/command evidence with an explanation. Never guess to fill gaps. If binding fails, do not claim success; retain existing evidence and explain why it was not bound.")
	}
	if guidance != "" || has["get_finding_traffic"] || has["update_finding_report"] {
		guidance += locale.Text(locale.First(langs), findingIDGuidance)
	}
	return out, guidance
}

func stripTrafficParameters(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	delete(props, "traffic_refs")
	delete(props, "evidence_hint_id")
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if key != "traffic_refs" && key != "evidence_hint_id" {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	if hints, ok := props["hints"].(map[string]any); ok {
		if items, ok := hints["items"].(map[string]any); ok {
			stripTrafficParameters(items)
		}
	}
}

// HintTrafficSchema is shared by the task-local and cross-task hint tools.
func HintTrafficSchema(langs ...locale.Lang) map[string]any {
	return map[string]any{"type": "array", "description": locale.Text(locale.First(langs), "Optional: verified traffic references supporting this hint's specific vulnerability, in order. After handoff, report_finding can use evidence_hint_id to include these references."), "items": obj(map[string]any{"traffic_id": str(locale.Text(locale.First(langs), "Actual traffic ID")), "role": str("baseline / proof / verification / supporting"), "note": str(locale.Text(locale.First(langs), "The conclusion this traffic supports"))}, "traffic_id")}
}

func (t *ToolSet) findingRefsFromHint(hintID int64, explicit []db.TrafficRef) ([]db.TrafficRef, error) {
	if hintID <= 0 {
		return db.NormalizeTrafficRefs(explicit)
	}
	n, err := t.ts.GetNode(hintID) // local store only: inherited hints cannot supply evidence
	if err != nil {
		return nil, err
	}
	if n == nil || n.Kind != db.KindHint {
		return nil, locale.Errorf("evidence_hint_id=%d must be a hint node from this task (inherited hints cannot be bound directly)", hintID)
	}
	var payload struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, err
	}
	return db.NormalizeTrafficRefs(append(append([]db.TrafficRef{}, explicit...), payload.Refs...))
}
