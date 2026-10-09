package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/agent"
	"github.com/sebastian93921/artifex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// jsonResult marshals v to a JSON tool result.
func jsonResult(v any) (actool.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	return actool.Text(string(b)), nil
}

// P2 cross-task orchestration host tools need access to Manager stores for arbitrary tasks,
// Engine pause controls, and task creation, so they belong in the server layer.
// Read tools redirect existing per-task tools to the target store through a temporary ToolSet,
// reusing identical logic. Control tools such as spawn/pause call Manager/Engine directly.
// Like traffic tools, these are seeded into tools and visible only to agents explicitly bound to them.

// hostTools is the runtime host-tool provider fed to ToolAugment: traffic tools
// (gated by capture) + cross-task orchestration tools + user-defined custom tools.
// The second return is the names of custom tools flagged `deferred` (schema
// withheld, routed via SearchExtraTools/ExecuteExtraTool). Per-agent binding still
// decides who actually sees any of them.
//
//nolint:unused // used as the hostTools provider in wireAgentAugment
func (s *Server) hostTools(ctx context.Context) ([]actool.CoreTool, map[string][]string) {
	tools := append(s.m.HostTools(locale.FromContext(ctx)), s.orchestrationTools(locale.FromContext(ctx))...)
	tools = append(tools, s.findingRetestTools(locale.FromContext(ctx))...)
	tools = append(tools, s.platformTools(locale.FromContext(ctx))...) // Platform tools for Auto to create/edit skills, tools, and MCP servers.
	custom, err := s.customToolsForLanguage(locale.FromContext(ctx))
	if err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[custom-tool] Loading failed: %v"), err)
		return tools, nil
	}
	tools = append(tools, custom...)
	// deferred custom tools → name -> its bound agent keys. ToolAugment turns a
	// name into a deferred entry only for agents it's actually bound to (so we don't
	// advertise a tool the per-agent binding will drop from the callable set).
	deferred := map[string][]string{}
	rows, _ := s.m.pg.ListCustomTools()
	for _, t := range rows {
		if t.Deferred && t.Enabled {
			deferred[t.Key] = t.Agents
		}
	}
	return tools, deferred
}

// orchestrationTools returns the cross-task tool set. Bound per-agent via the
// tools table (default: no binding — opt-in for orchestration agents).
func (s *Server) orchestrationTools(langs ...locale.Lang) []actool.CoreTool {
	return []actool.CoreTool{
		s.toolListTasks(langs...),
		s.toolListLLMProfiles(langs...),
		s.toolSpawnTask(langs...),
		s.toolPauseTask(langs...),
		s.toolGetTaskGraph(langs...),
		s.toolListTaskFindings(langs...),
		s.toolAddHint(langs...),
		s.toolGetWorkerTrace(langs...),
		s.toolListWorkerTraces(langs...),
		s.toolSearchWorkerTraces(langs...),
		s.toolGetTaskNodeDetail(langs...),
		s.toolUpdateFindingReport(langs...),
		s.toolGetFindingTraffic(langs...),
		s.toolBindFindingTraffic(langs...),
	}
}

// --- schema helpers ---

func strParam(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// parseProfileID reads an LLM profile id from a tool arg that may arrive as a JSON
// number (5) or a numeric string ("5"); returns 0 when absent/unparseable.
func parseProfileID(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		v, _ := strconv.ParseInt(strings.TrimSpace(str), 10, 64)
		return v
	}
	return 0
}

func objSchema(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		m["required"] = req
	}
	return m
}

func roTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

func wrTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

// delegateToTask resolves the `task_id` in the input, builds a ToolSet bound to
// that task's store, strips task_id, and calls the chosen per-task tool — so the
// cross-task read reuses the exact in-task logic against another task.
func (s *Server) delegateToTask(ctx context.Context, in json.RawMessage, pick func(*agent.ToolSet) actool.CoreTool) (actool.Result, error) {
	var head struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(in, &head)
	if strings.TrimSpace(head.TaskID) == "" {
		return actool.Errorf(locale.Text(locale.FromContext(ctx), "task_id is required")), nil
	}
	t, ok := s.m.Task(head.TaskID)
	if !ok {
		return actool.Errorf(locale.Text(locale.FromContext(ctx), "Task not found: ") + head.TaskID), nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(in, &m)
	delete(m, "task_id")
	inner, _ := json.Marshal(m)
	tsx := agent.NewToolSet(t.Store, "orchestrator", locale.FromContext(ctx))
	if s.m.Assets() != nil {
		tsx.SetAssetStore(s.m.Assets(), s.m.Assets().Companies())
	}
	tsx.SetNotify(t.Notify)         // General wakeup for writes without a dedicated callback; read tools use a no-op.
	tsx.SetNotifyHint(t.NotifyHint) // add_hint records a user-added strategic-hint trigger and wakes the planner.
	return pick(tsx).Call(ctx, inner, nil)
}

// --- tools ---

func (s *Server) toolListTasks(langs ...locale.Lang) actool.CoreTool {
	return roTool("list_tasks",
		locale.Text(locale.First(langs), "List all tasks with id/description/objective/state/elapsed time/parent/LLM profile, so orchestration can inspect progress, stalls, and model assignments. Elapsed seconds run from creation to now for active tasks or last activity for terminal tasks. llm_profile is the planner/worker profile name; active profile means following the global selection."),
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			lastAct, _ := s.m.PG().LastActivityAll()
			// id -> name to resolve each task's pinned LLM profile.
			profName := map[int64]string{}
			if profs, err := s.m.pg.ListProfiles(); err == nil {
				for _, p := range profs {
					profName[p.ID] = p.Name
				}
			}
			out := make([]map[string]any, 0)
			for _, t := range s.m.List() {
				status := s.deriveTaskStatus(t)
				end := lastAct[t.ExpID]
				if live := s.engine.LastActivity(t.ID); live > end {
					end = live
				}
				dur := int64(0)
				if status == "running" {
					dur = time.Now().Unix() - t.CreatedAt
				} else if end > t.CreatedAt {
					dur = end - t.CreatedAt
				}
				row := map[string]any{"id": t.ID, "description": t.Description, "goal": t.Goal, "status": status, "run_seconds": dur}
				if t.ParentRef != "" {
					row["parent_ref"] = t.ParentRef
				}
				llmState := t.llmStateSnapshot()
				if llmState.ProfileID == nil {
					row["llm_profile"] = locale.Text(locale.First(langs), "(active profile)")
				} else if n, ok := profName[*llmState.ProfileID]; ok {
					row["llm_profile"] = n
				} else {
					row["llm_profile"] = fmt.Sprintf(locale.Text(locale.First(langs), "#%d (deleted)"), *llmState.ProfileID)
				}
				out = append(out, row)
			}
			return jsonResult(out)
		})
}

// toolListLLMProfiles lists the available LLM profiles (name/model/active) so an
// orchestration agent can pick one for spawn_task's llm_profile. Never leaks keys.
func (s *Server) toolListLLMProfiles(langs ...locale.Lang) actool.CoreTool {
	return roTool("list_llm_profiles",
		locale.Text(locale.First(langs), "List available LLM profiles: id, name, model, format, and active status. Use an ID as spawn_task.llm_profile_id to assign a child task's model. API keys are never included."),
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			profs, err := s.m.pg.ListProfiles()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			out := make([]map[string]any, 0, len(profs))
			for _, p := range profs {
				out = append(out, map[string]any{
					"id": p.ID, "name": p.Name, "model": p.Model, "format": p.Format, "is_active": p.IsDefault,
				})
			}
			return jsonResult(map[string]any{"profiles": out})
		})
}

func (s *Server) toolSpawnTask(langs ...locale.Lang) actool.CoreTool {
	return wrTool("spawn_task",
		locale.Text(locale.First(langs), "Create a child task and start its exploration engine, returning task_id. Delegate one objective as an independent task. Optional parent_ref associates it with the current parent task."),
		objSchema(map[string]any{
			"description":            strParam(locale.Text(locale.First(langs), "Task description, a short title")),
			"goal":                   strParam(locale.Text(locale.First(langs), "Task objective: the outcome to achieve")),
			"parent_ref":             strParam(locale.Text(locale.First(langs), "Optional parent task ID for parent-child association")),
			"source_task_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": fmt.Sprintf(locale.Text(locale.First(langs), "Optional read-only source task IDs, at most %d. The child may reference their established assets/conclusions as starting context. Unlike parent_ref, this is content inheritance."), db.MaxTaskSourceCount)},
			"llm_profile_id":         map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Optional LLM profile ID for this child's planner/worker, from list_llm_profiles; otherwise inherit the parent's profile, then the globally active profile")},
			"timeout_seconds":        map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Optional task timeout in seconds; triggers graceful settlement and terminal timeout state. Omitted/0 means unlimited.")},
			"plan_heartbeat_seconds": map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Optional planner heartbeat in seconds: if no trigger arrives since the last planning round/task start, wake planning to avoid stalls and supervise workers. Omitted/0 uses the configured default.")},
			"seed_first_intent":      map[string]any{"type": "boolean", "description": locale.Text(locale.First(langs), "Optional for simple tasks: create an initial intent from description+goal so a worker starts without waiting for the first planner round. Default false uses the normal plan-then-execute flow.")},
		}, "description", "goal"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Description          string          `json:"description"`
				Goal                 string          `json:"goal"`
				ParentRef            string          `json:"parent_ref"`
				SourceTaskIDs        []string        `json:"source_task_ids"`
				LLMProfileID         json.RawMessage `json:"llm_profile_id"`
				TimeoutSeconds       int             `json:"timeout_seconds"`
				PlanHeartbeatSeconds int             `json:"plan_heartbeat_seconds"`
				SeedFirstIntent      bool            `json:"seed_first_intent"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.Description) == "" {
				a.Description = locale.Text(locale.First(langs), "Untitled task")
			}
			if strings.TrimSpace(a.Goal) == "" {
				return actool.Errorf(locale.Text(locale.First(langs), "goal is required")), nil
			}
			if a.TimeoutSeconds < 0 {
				a.TimeoutSeconds = 0
			}
			// Validate read-only inheritance sources: count limit and valid, unique, existing IDs, matching HTTP task creation.
			if len(a.SourceTaskIDs) > db.MaxTaskSourceCount {
				return actool.Errorf(fmt.Sprintf(locale.Text(locale.First(langs), "At most %d source tasks may be selected"), db.MaxTaskSourceCount)), nil
			}
			sourceIDs := make([]int64, 0, len(a.SourceTaskIDs))
			seenSources := map[int64]bool{}
			for _, raw := range a.SourceTaskIDs {
				id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				if err != nil || id <= 0 || seenSources[id] {
					return actool.Errorf(locale.Text(locale.First(langs), "Source task ID is invalid or duplicated")), nil
				}
				if _, ok := s.m.Task(strconv.FormatInt(id, 10)); !ok {
					return actool.Errorf(fmt.Sprintf(locale.Text(locale.First(langs), "Source task #%d does not exist"), id)), nil
				}
				seenSources[id] = true
				sourceIDs = append(sourceIDs, id)
			}
			// LLM profile resolution: explicit id > inherit parent's pin > active(nil).
			var pin *int64
			if id := parseProfileID(a.LLMProfileID); id > 0 {
				if _, ok := s.loadProfileConfig(id); !ok {
					return actool.Errorf(fmt.Sprintf(locale.Text(locale.First(langs), "LLM profile #%d does not exist or has no API key"), id)), nil
				}
				pin = &id
			} else if a.ParentRef != "" {
				if pt, ok := s.m.Task(a.ParentRef); ok {
					pin = pt.LLMProfileID
				}
			}
			var llmIDs []int64
			if pin != nil {
				llmIDs = []int64{*pin}
			}
			t, err := s.m.CreateTaskWithOptions(a.Description, a.Goal, db.TaskCreateOptions{
				Language:             s.childTaskLanguage(ctx, a.ParentRef),
				SourceTaskIDs:        sourceIDs,
				LLMProfileIDs:        llmIDs,
				TimeoutSeconds:       a.TimeoutSeconds,
				PlanHeartbeatSeconds: a.PlanHeartbeatSeconds,
			})
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if a.ParentRef != "" {
				t.ParentRef = a.ParentRef
				if id, e := strconv.ParseInt(t.ID, 10, 64); e == nil {
					_ = s.m.PG().SetParentRef(id, a.ParentRef)
				}
			}
			// Share launchTask with HTTP createTask in server.go:
			// seed, emit background goal decomposition (round zero/LLM steps/goals), then engine.Run.
			// seed_first_intent defaults false for planning before execution; simple tasks may directly dispatch one test work item.
			s.launchTask(t, a.Description+" "+a.Goal, a.SeedFirstIntent)
			return actool.Text(fmt.Sprintf("task created: %s", t.ID)), nil
		})
}

func (s *Server) toolPauseTask(langs ...locale.Lang) actool.CoreTool {
	return wrTool("pause_task", locale.Text(locale.First(langs), "Pause a task and stop its planner/worker loops."),
		objSchema(map[string]any{"task_id": strParam(locale.Text(locale.First(langs), "Task ID to pause"))}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TaskID string `json:"task_id"`
			}
			_ = json.Unmarshal(in, &a)
			t, ok := s.m.Task(a.TaskID)
			if !ok {
				return actool.Errorf(locale.Text(locale.First(langs), "Task not found: ") + a.TaskID), nil
			}
			if _, err := s.applyTaskControlWithCause(t, "pause", agent.AbortPausedByOrchestrator); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return actool.Text("task paused: " + a.TaskID), nil
		})
}

func (s *Server) toolGetTaskGraph(langs ...locale.Lang) actool.CoreTool {
	return roTool("get_task_graph", locale.Text(locale.First(langs), "Read a task's graph overview by task_id: asset counts, frontier, findings, coverage, and related situation data, like graph_overview."),
		objSchema(map[string]any{"task_id": strParam(locale.Text(locale.First(langs), "Task ID"))}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GraphOverviewTool)
		})
}

func (s *Server) toolListTaskFindings(langs ...locale.Lang) actool.CoreTool {
	return roTool("list_task_findings", locale.Text(locale.First(langs), "Read confirmed findings for task_id, including flags/PoC and id/task_id/intent_id/vulnclass/severity/summary/status."),
		objSchema(map[string]any{"task_id": strParam(locale.Text(locale.First(langs), "Task ID"))}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListFindingsTool)
		})
}

func (s *Server) toolAddHint(langs ...locale.Lang) actool.CoreTool {
	return wrTool("add_task_hint", locale.Text(locale.First(langs), "Add strategic hints to a task for its planner's next round.\n")+
		locale.Text(locale.First(langs), "Prefer one hints batch; returned ids match length/order and failed entries are 0. For one hint, omit hints and provide top-level text."),
		objSchema(map[string]any{
			"task_id":      strParam(locale.Text(locale.First(langs), "Task ID")),
			"hints":        map[string]any{"type": "array", "description": locale.Text(locale.First(langs), "Preferred hints array; each item uses text/asset_ids/traffic_refs."), "items": objSchema(map[string]any{"text": strParam(locale.Text(locale.First(langs), "Hint text")), "asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "traffic_refs": agent.HintTrafficSchema(locale.First(langs))})},
			"text":         strParam(locale.Text(locale.First(langs), "Single hint text")),
			"traffic_refs": agent.HintTrafficSchema(locale.First(langs)),
			"asset_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(locale.First(langs), "Optional anchored asset IDs in that task (zero/one/many)")},
		}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).AddHintTool)
		})
}

func (s *Server) toolGetWorkerTrace(langs ...locale.Lang) actool.CoreTool {
	return roTool("get_task_worker_trace",
		locale.Text(locale.First(langs), "Inspect a worker intent's trace in a task. get_task_worker_trace(task_id,intent_id) returns step summaries; add step_ids for full content, at most the first five per call."),
		objSchema(map[string]any{
			"task_id":   strParam(locale.Text(locale.First(langs), "Task ID")),
			"intent_id": map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Intent ID for work in that task")},
			"step_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(locale.First(langs), "Optional step IDs for full content, at most five; excess IDs appear in omitted_step_ids")},
		}, "task_id", "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GetWorkerTraceTool)
		})
}

func (s *Server) toolListWorkerTraces(langs ...locale.Lang) actool.CoreTool {
	return roTool("list_task_worker_traces", locale.Text(locale.First(langs), "List executed intents and step counts for a task to identify traces worth inspecting with get_task_worker_trace."),
		objSchema(map[string]any{"task_id": strParam(locale.Text(locale.First(langs), "Task ID"))}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListWorkerTracesTool)
		})
}

func (s *Server) toolSearchWorkerTraces(langs ...locale.Lang) actool.CoreTool {
	return roTool("search_task_worker_traces", locale.Text(locale.First(langs), "Search all worker traces in a task by keyword, returning matching step summaries and intent_id."),
		objSchema(map[string]any{"task_id": strParam(locale.Text(locale.First(langs), "Task ID")), "q": strParam(locale.Text(locale.First(langs), "Search keyword"))}, "task_id", "q"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).SearchWorkerTracesTool)
		})
}

func (s *Server) toolGetTaskNodeDetail(langs ...locale.Lang) actool.CoreTool {
	return roTool("get_task_node_detail",
		locale.Text(locale.First(langs), "Read full exploration-node content for a task (finding/fact/intent/goal: summary, detail, evidence, PoC). id is the exploration node ID from report_finding or list_task_findings, not an asset ID. Read full finding evidence before writing its report."),
		objSchema(map[string]any{
			"task_id": strParam(locale.Text(locale.First(langs), "Task ID")),
			"id":      map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Exploration node ID, not an asset ID")},
		}, "task_id", "id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).NodeDetailTool)
		})
}

// toolUpdateFindingReport writes/overwrites a finding's detailed Markdown report.
// finding_id is the id report_finding returned ("finding recorded: <id>", the
// finding node id). The write (SetFindingReportByNodeID) is keyed by node_id and
// task-agnostic, so this host tool needs no task_id / exploration store.
func (s *Server) toolUpdateFindingReport(langs ...locale.Lang) actool.CoreTool {
	return wrTool("update_finding_report",
		locale.Text(locale.First(langs), "Write/update a registered finding's detailed Markdown report, replacing the previous report. finding_id takes the exploration node ID in report_finding's \"finding recorded: <id>\" line. Include overview, impact, reproduction, evidence/PoC, and remediation."),
		objSchema(map[string]any{
			"finding_id":       map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Target exploration finding node ID returned by report_finding")},
			"report":           strParam(locale.Text(locale.First(langs), "Complete detailed report in Markdown")),
			"evidence_version": map[string]any{"type": "integer", "description": locale.Text(locale.First(langs), "Evidence version returned by get_finding_traffic, preventing reports from overwriting newer evidence changes")},
		}, "finding_id", "report"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				EvidenceVersion *int64          `json:"evidence_version"`
				FindingID       json.RawMessage `json:"finding_id"`
				Report          string          `json:"report"`
			}
			_ = json.Unmarshal(in, &a)
			nodeID := parseProfileID(a.FindingID) // Reuse number-or-numeric-string parsing.
			if nodeID <= 0 {
				return actool.Errorf(locale.Text(locale.First(langs), "Invalid finding_id")), nil
			}
			n, err := s.m.pg.SetFindingReportVersionByNodeID(ctx, nodeID, a.Report, a.EvidenceVersion)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if n == 0 {
				return actool.Errorf(fmt.Sprintf(locale.Text(locale.First(langs), "No finding record for finding_id=%d; register it first with report_finding"), nodeID)), nil
			}
			return actool.Text(fmt.Sprintf("finding %d report updated (%d chars)", nodeID, len(a.Report))), nil
		})
}

// deriveTaskStatus mirrors listTasks' status derivation for the list_tasks tool.
func (s *Server) deriveTaskStatus(t *Task) string {
	lifecycle := t.lifecycleSnapshot()
	switch {
	case isTerminalStatus(lifecycle.Status):
		return lifecycle.Status
	case lifecycle.Paused || s.engine.IsPaused(t.ID):
		return "paused"
	case s.engine.ReadyFor(t) && s.engine.Started(t.ID):
		return "running"
	}
	return "created"
}

// orchestrationToolSeeds seeds the cross-task tools into the tools table so they
// are bindable per-agent (default: bound to nobody — opt-in for orchestration
// agents). First-insert only, like the traffic seeds.
func (s *Server) seedOrchestrationTools() {
	// Task-operation and platform tools bind to the built-in Auto agent, which operates the platform.
	// SeedTool applies only on insertion; seedAutoDefaultBindings adds bindings to previously seeded databases.
	autoAgents, _ := json.Marshal([]string{"auto"})
	for _, t := range s.orchestrationTools(locale.En) {
		schema, _ := json.Marshal(t.InputSchema())
		bindings := autoAgents
		if t.Name() == "bind_finding_traffic" {
			bindings = json.RawMessage(`["reporter"]`)
		}
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, bindings)
	}
	for _, t := range s.platformTools(locale.En) {
		schema, _ := json.Marshal(t.InputSchema())
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, autoAgents)
	}
	s.refreshBuiltinToolSchemas()
	s.seedAutoDefaultBindings()
	s.seedPlannerDefaultBindings()
	s.seedPlannerListAssetsBinding()
	s.seedCompanyScopeRebind()
	s.seedWorkerReadToolsUnbind() // One-time removal of worker defaults for list_facts/list_companies/list_worker_traces.
	s.seedWorkerReadbackRebind()  // One-time repair of prior migration: rebind search_all_worker_traces/get_worker_trace/node_detail to worker.
	s.seedAutoReportFindingBinding()
	s.unbindGoalMetDefault()
	s.reseedGoalsPrompt()             // One-time goals prompt version adding operation-constraint extraction.
	s.reseedMainAgentPrompt()         // One-time main-agent prompt guidance to ask about goals when adding intents after completion.
	s.reseedPlannerPrompt()           // One-time planner prompt revision for justified zero-intent rounds and quantitative acceptance checks.
	s.reseedWorkerPrompt()            // One-time worker prompt revision adding evidence requirements for negative conclusions.
	s.seedReporterAgent()             // One-time report-writing agent, tool bindings, and finding trigger seed.
	s.upgradeReporterTriggerMessage() // One-time reporter migration to return evidence_version.
	s.seedFindingTrafficTools()       // Add optional evidence parameters and read-only evidence tools while preserving user configuration.
	s.seedFindingWorkflowTools()
	// Pentest default bindings need no migration: fresh BuiltinToolSeeds already includes
	// list_assets/insert_assets/report_finding/list_findings/list_companies for pentest.
	// No historical database existed when this binding set was introduced.
}

// refreshBuiltinToolSchemas propagates code schema/description changes on the
// orchestration + platform tools into already-seeded rows ONCE per version flag —
// SeedTool is first-insert-only, so new parameters such as spawn_task's llm_profile never
// reaches an old DB otherwise. Preserves each tool's agent binding + enabled flag.
// Bump the flag whenever these tools' schemas/descriptions change in code.
func (s *Server) refreshBuiltinToolSchemas() {
	const flag = "tool_schema_refresh_v7_list_facts_paging"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	tools := append(s.orchestrationTools(locale.En), s.platformTools(locale.En)...)
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema())
		if err := s.m.pg.RefreshToolDefaults(t.Name(), t.Description(), schema); err != nil {
			log.Printf("[tools] refresh %s schema failed: %v", t.Name(), err)
		}
	}
	// Also refresh selected built-in agent tools to their code defaults:
	// - goal_met: older descriptions misleadingly said to end the planning round, allowing the planner
	//   to mistake an empty round for completion of the whole task immediately after startup.
	// - insert_assets: the new related argument controls task relevance and coverage inclusion;
	//   first-insert-only seeding would leave existing schemas without it.
	// - list_facts: pagination adds limit/before/q; otherwise older empty schemas show no parameters
	//   in tool management and provide no parameter descriptions to the model.
	refreshBuiltin := map[string]bool{"goal_met": true, "insert_assets": true, "list_facts": true}
	for _, sd := range agent.BuiltinToolSeeds() {
		if !refreshBuiltin[sd.Key] {
			continue
		}
		schema, _ := json.Marshal(sd.Schema)
		if err := s.m.pg.RefreshToolDefaults(sd.Key, sd.Desc, schema); err != nil {
			log.Printf("[tools] refresh %s desc failed: %v", sd.Key, err)
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
	log.Print(locale.Text(locale.ServerDefault(), "[tools] Refreshed orchestration/platform schemas to code defaults (one-time)"))
}

// unbindGoalMetDefault removes goal_met's default "planner" binding ONCE (guarded by
// a settings flag), so existing DBs match the new default of NO agent. goal_met bypasses
// per-goal prove_goal to declare the whole task done — powerful/risky and redundant with
// the prove_goal→auto-complete path — so it ships unbound; users can re-bind it per agent
// in the UI. A user's own binding to another agent is untouched (we only strip planner).
func (s *Server) unbindGoalMetDefault() {
	const flag = "goal_met_unbind_default_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.RemoveAgentFromTool("planner", "goal_met"); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[tools] Could not unbind goal_met from planner: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// reseedGoalsPrompt refreshes the goals decomposer to the current code default, which adds constraint
// extraction through set_constraints before goal decomposition. First-insert-only SeedPromptIfEmpty
// cannot update existing version 1, so ResetPromptToDefault appends and selects a new version.
// Old versions remain recoverable in history, including customizations. A settings flag permits one attempt;
// bump it for future default changes. Fresh databases already receive the latest default during seeding.
func (s *Server) reseedGoalsPrompt() {
	const flag = "goals_prompt_constraint_step_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once regardless of success.
	a, err := s.m.pg.GetAgentByKey("goals")
	if err != nil || a == nil {
		return // No agent row yet: fresh-database seedPrompts will install the latest default without migration.
	}
	tmpl := agent.BuiltinPromptSeeds()["goals"]
	if tmpl == "" {
		return
	}
	// Fresh seedPrompts already installed the current default; avoid appending a duplicate version.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[prompts] Could not refresh the goals default: %v"), err)
		return
	}
	log.Print(locale.Text(locale.ServerDefault(), "[prompts] Added goals default version with constraint extraction (one-time)"))
}

// reseedMainAgentPrompt refreshes the main-agent code default, adding guidance to ask whether
// manually submitted intents after goal completion should become formal goals. First-insert-only
// SeedPromptIfEmpty cannot update existing versions; ResetPromptToDefault appends and selects a new one.
// Previous customizations remain recoverable in history. A settings flag allows one attempt; fresh databases
// already receive the latest default. This follows the same structure as reseedGoalsPrompt.
func (s *Server) reseedMainAgentPrompt() {
	const flag = "mainagent_prompt_goalless_intent_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once regardless of success.
	a, err := s.m.pg.GetAgentByKey("mainagent")
	if err != nil || a == nil {
		return // No agent row yet: fresh-database seedPrompts will install the latest default without migration.
	}
	tmpl := agent.BuiltinPromptSeeds()["mainagent"]
	if tmpl == "" {
		return
	}
	// Fresh seedPrompts already installed the current default; avoid appending a duplicate version.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[prompts] Could not refresh the mainagent default: %v"), err)
		return
	}
	log.Print(locale.Text(locale.ServerDefault(), "[prompts] Added mainagent default version with confirmation before new post-completion goals (one-time)"))
}

// reseedPlannerPrompt refreshes the simplified planner default: restraint means deduplication only,
// depth takes priority over coverage, unmet goals with no active intents require output, and negative reviews are bounded.
// Bump the flag (currently v2) for substantive changes. First-insert-only seeding cannot update existing versions,
// so ResetPromptToDefault appends and selects a new version while preserving older customizations
// in history. A settings flag permits one attempt; fresh databases already have the latest default.
// This follows the same structure as reseedGoalsPrompt.
func (s *Server) reseedPlannerPrompt() {
	const flag = "planner_prompt_compact_realistic_v2"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once regardless of success.
	a, err := s.m.pg.GetAgentByKey("planner")
	if err != nil || a == nil {
		return // No agent row yet: fresh-database seedPrompts will install the latest default without migration.
	}
	tmpl := agent.BuiltinPromptSeeds()["planner"]
	if tmpl == "" {
		return
	}
	// Fresh seedPrompts already installed the current default; avoid appending a duplicate version.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[prompts] Could not refresh the planner default: %v"), err)
		return
	}
	log.Print(locale.Text(locale.ServerDefault(), "[prompts] Added planner default with deduplication, depth priority, and bounded negative rechecks (one-time)"))
}

// reseedWorkerPrompt refreshes record_fact guidance: remove the negative-conclusion observation/tentative-reading
// instruction and decouple observed/inferred confidence from exhausting an intent's methods, avoiding planner confusion.
// Separate facts-array entries are limited to truly independent, unmergeable cases. Flag v3 updates existing databases.
// First-insert-only seeding cannot update existing versions, so append/select a version while keeping history recoverable.
// A settings flag permits one attempt; fresh databases need no migration. Same structure as reseedGoalsPrompt.
func (s *Server) reseedWorkerPrompt() {
	const flag = "worker_prompt_compact_v4"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once regardless of success.
	a, err := s.m.pg.GetAgentByKey("worker")
	if err != nil || a == nil {
		return // No agent row yet: fresh-database seedPrompts will install the latest default without migration.
	}
	tmpl := agent.BuiltinPromptSeeds()["worker"]
	if tmpl == "" {
		return
	}
	// Fresh seedPrompts already installed the current default; avoid appending a duplicate version.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[prompts] Could not refresh the worker default: %v"), err)
		return
	}
	log.Print(locale.Text(locale.ServerDefault(), "[prompts] Added worker default with focused list_assets/list_findings context guidance (one-time)"))
}

// reporterToolCallMessage must always require get_finding_traffic before writing the report.
// This read-only tool works regardless of capture/automatic-binding settings and can read manually bound evidence.
// Conditional guidance would omit evidence_version when automatic binding is disabled by default,
// causing SetFindingReportVersionByNodeID to store legacy -1. Finding details and Markdown exports
// would then permanently show changed evidence awaiting a report, with no UI action to clear it.
const reporterToolCallMessage = "A finding was just registered by report_finding. Read finding_id (independent finding record) and finding_node_id (exploration node) from the returned JSON. " +
	"Use get_finding_traffic(finding_id) to read the evidence list and version; an empty list is valid and does not block reporting. " +
	"If run guidance enables automatic binding, verify and bind this finding's traffic before reading. Use finding_node_id for node details. " +
	"Finally save with update_finding_report(finding_id=finding_node_id, report, evidence_version=the version actually read). " +
	"evidence_version is required or the report remains stale. Never mix the two ID namespaces."

// Legacy trigger message from 0.3.8 or earlier. Migrate exact matches only; preserve user edits.
const reporterToolCallMessageV1 = "A finding was just registered by report_finding. Extract its finding_id " +
	"(the number in \"finding recorded: <id>\") and task ID from the trigger context, write its detailed report, " +
	"then save with update_finding_report(finding_id, report)."

// upgradeReporterTriggerMessage updates unchanged stock reporter trigger text in older databases.
// seedReporterAgent writes triggers only when creating the agent and is guarded by reporter_agent_seed_v1,
// so upgraded databases otherwise miss the new text. seedFindingTrafficTools adds evidence_version to schemas,
// but the reporter needs instructions to use it. This one-time migration changes only untouched defaults.
func (s *Server) upgradeReporterTriggerMessage() {
	const flag = "reporter_trigger_evidence_version_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt only once.
	triggers, err := s.m.pg.ListTriggersFor("reporter")
	if err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not read triggers: %v"), err)
		return
	}
	for _, t := range triggers {
		if !t.OnToolCall || !isLegacyReporterTrigger(t.ToolCallMessage) {
			continue // Preserve user edits and non-finding triggers.
		}
		t.ToolCallMessage = reporterToolCallMessage
		if err := s.m.pg.UpdateTrigger(t); err != nil {
			log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not upgrade trigger message: %v"), err)
			return
		}
		log.Print(locale.Text(locale.ServerDefault(), "[reporter] Trigger message now reads and returns evidence_version"))
	}
}

// seedReporterAgent creates an editable/deletable report-writing custom agent (builtin=false),
// binds update_finding_report and task-query tools, and triggers it after each report_finding call
// to write a detailed report. A one-time settings flag prevents recreation after user deletion.
// Orchestration tools were seeded above, so their bindings are available.
func (s *Server) seedReporterAgent() {
	const flag = "reporter_agent_seed_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once regardless of success.

	if exist, _ := s.m.pg.GetAgentByKey("reporter"); exist != nil {
		return // Preserve an existing key created by the user.
	}
	a, err := s.m.pg.CreateAgent("reporter", "Report writer",
		"Writes detailed vulnerability reports: triggered by findings, reads evidence and execution traces, then saves a Markdown report.")
	if err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not create agent: %v"), err)
		return
	}
	if err := s.m.pg.SeedPromptIfEmpty(a.ID, agent.ReporterDefaultPrompt); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not seed prompt: %v"), err)
	}
	// Trigger policy parallel + none gives each finding its own concurrent report run.
	// Merge must be none: the default all would combine a burst of findings and defeat parallel runs.
	// maxParallel=5 limits concurrent report conversations and avoids a burst of LLM calls.
	if err := s.m.pg.SetAgentTriggerBehavior("reporter", "parallel", "none", 5); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not set trigger behavior: %v"), err)
	}
	// Bind report writing and read-only evidence, execution-trace, and task-state tools.
	if err := s.m.pg.AddAgentToToolBinding("reporter", []string{
		"update_finding_report", "get_task_node_detail", "list_task_findings",
		"get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces",
		"get_task_graph",
	}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not bind tools: %v"), err)
	}
	// Trigger on report_finding calls; its "finding recorded: <id>" result supplies finding_id,
	// and the trigger message also includes task_id.
	if _, err := s.m.pg.CreateTrigger(&db.AgentTrigger{
		AgentKey:        "reporter",
		Enabled:         true,
		OnToolCall:      true,
		ToolNames:       []string{"report_finding"},
		ToolCallMessage: reporterToolCallMessage,
	}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[reporter] Could not create trigger: %v"), err)
	}
	log.Print(locale.Text(locale.ServerDefault(), "[reporter] Seeded report-writer agent and finding trigger"))
}

// seedAutoReportFindingBinding adds "auto" to report_finding's binding ONCE so
// conversation-context agents can call it without requiring an intent_id.
func (s *Server) seedAutoReportFindingBinding() {
	const flag = "auto_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("auto", []string{"report_finding"}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[auto] Could not bind report_finding by default: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedPlannerDefaultBindings adds "planner" to report_finding's binding ONCE
// (guarded by a settings flag), so existing DBs — whose report_finding row was
// seeded as worker-only — also let the planner record findings. Fresh DBs already
// get it via PlannerTools(); this only backfills without overriding a user unbind.
func (s *Server) seedPlannerDefaultBindings() {
	const flag = "planner_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"report_finding"}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[planner] Could not bind report_finding by default: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedPlannerListAssetsBinding adds "planner" to list_assets's binding ONCE
// (guarded by a settings flag), so existing DBs — whose list_assets row was seeded
// as auto/pentest-only — also let the planner query the asset store by DSL. Fresh
// DBs already get it via PlannerTools(); this only backfills without overriding a
// user unbind.
func (s *Server) seedPlannerListAssetsBinding() {
	const flag = "planner_list_assets_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"list_assets"}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[planner] Could not bind list_assets by default: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedCompanyScopeRebind changes add_company_scope's default binding ONCE on
// existing DBs (guarded by a settings flag): the tool moves off worker and onto
// planner — defining a company's asset scope is a planning/main/auto concern, not
// something a worker does mid-exploration. Fresh DBs already get planner via
// PlannerTools() and lack worker via WorkerTools(); this only backfills old rows.
// One-shot + flag-guarded so a user who later re-binds worker isn't overridden.
func (s *Server) seedCompanyScopeRebind() {
	const flag = "company_scope_rebind_v1" // Switch default bindings from worker to planner.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"add_company_scope"}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[planner] Could not bind add_company_scope by default: %v"), err)
		return
	}
	if err := s.m.pg.RemoveAgentFromTool("worker", "add_company_scope"); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[worker] Could not unbind add_company_scope: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedWorkerReadToolsUnbind strips the read-context tools off worker's default
// binding ONCE on existing DBs (guarded by a settings flag): a worker executes one
// intent and writes back — reading facts/companies and listing all workers' traces is
// a planning/main concern, not the executor's. Fresh DBs already lack these via
// WorkerTools(); this only backfills old rows without overriding a user who
// deliberately re-binds worker. Each RemoveAgentFromTool is per-tool +
// membership-guarded, so planner/mainagent bindings of the same tool are untouched.
//
// NOTE: search_all_worker_traces / get_worker_trace / node_detail are intentionally NOT
// unbound — worker owns them for cross-work look-back + node drill-down (see WorkerTools).
// They used to be in this list back when worker lacked them; seedWorkerReadbackRebind
// repairs DBs whose old run stripped them.
func (s *Server) seedWorkerReadToolsUnbind() {
	const flag = "worker_readtools_unbind_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	for _, k := range []string{
		"list_facts", "list_companies", "list_worker_traces",
	} {
		if err := s.m.pg.RemoveAgentFromTool("worker", k); err != nil {
			log.Printf(locale.Text(locale.ServerDefault(), "[worker] Could not unbind %s: %v"), k, err)
			return // Do not set the flag on error; retry at the next startup.
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedWorkerReadbackRebind re-binds the cross-work look-back / drill-down tools onto
// worker ONCE (guarded by a settings flag): an earlier seedWorkerReadToolsUnbind wrongly
// stripped search_all_worker_traces / get_worker_trace / node_detail from worker after
// they had been added to WorkerTools(), so any DB that ran that migration lost them.
// Fresh DBs already have them via WorkerTools() and this is a harmless no-op there.
// One-shot + flag-guarded so a user who later deliberately unbinds them isn't overridden.
func (s *Server) seedWorkerReadbackRebind() {
	const flag = "worker_readback_rebind_v2" // v2 adds node_detail.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("worker", []string{
		"search_all_worker_traces", "get_worker_trace", "node_detail",
	}); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[worker] Could not bind trace/detail tools: %v"), err)
		return // Do not set the flag on error; retry at the next startup.
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedAutoDefaultBindings adds "auto" to the task-op + platform tools' bindings
// ONCE (guarded by a settings flag), so existing DBs whose tool rows were seeded
// before Auto existed still give Auto its default toolset — without re-adding it
// after a user deliberately unbinds.
func (s *Server) seedAutoDefaultBindings() {
	const flag = "auto_default_bindings_v3" // v3 replaces old asset-tool names and adds insert_assets/add_company_scope.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	keys := make([]string, 0, len(platformToolKeys)+12)
	for _, t := range s.orchestrationTools(locale.En) {
		keys = append(keys, t.Name())
	}
	keys = append(keys, platformToolKeys...)
	// Auto commonly reads/registers assets and manages company scope while operating the platform.
	keys = append(keys, "insert_assets", "add_company_scope", "list_assets")
	if err := s.m.pg.AddAgentToToolBinding("auto", keys); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[auto] Default tool binding failed: %v"), err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// Match the exact historical stock trigger without rewriting custom trigger text.
func isLegacyReporterTrigger(text string) bool {
	if text == reporterToolCallMessageV1 {
		return true
	}
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:]) == "c10b22c2daf51a0e4998fe0e84a2e22baf70927a6f7368874e1e1e0b0eacb02b"
}
