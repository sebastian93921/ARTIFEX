package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"strconv"
	"strings"

	"github.com/sebastian93921/artifex/db"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// compactIntents distills intents to {id, summary, state, asset_ids, parents,
// yields} so the planner sees both the direction and its LINEAGE — parents (the
// upstream nodes it derived from: facts/intents/findings) and yields (the facts/
// findings it produced) — without pulling full payloads. parentsOf/yieldsOf are
// built from the exploration edges in graph_overview.
func compactIntents(ns []*db.Node, parentsOf, yieldsOf map[int64][]int64) []map[string]any {
	out := make([]map[string]any, 0, len(ns))
	for _, n := range ns {
		var p map[string]any
		_ = json.Unmarshal(n.Payload, &p)
		m := map[string]any{"id": n.ID, "summary": p["summary"], "state": n.State}
		if n.Inherited {
			m["source_task_id"] = n.SourceTaskID
			m["inherited"] = true
		}
		// asset_ids is the structured "which assets this direction covers" signal for
		// dedup; fall back to legacy payload keys (target_ids plural, then target_id
		// single) so intents stored before the rename still surface their anchors.
		if tg, ok := p["asset_ids"]; ok && tg != nil {
			m["asset_ids"] = tg
		} else if tg, ok := p["target_ids"]; ok && tg != nil {
			m["asset_ids"] = tg
		} else if tg, ok := p["target_id"]; ok && tg != nil && tg != "" {
			m["asset_ids"] = []any{tg}
		}
		if ps := parentsOf[n.ID]; len(ps) > 0 {
			m["parents"] = ps // Upstream nodes deriving this intent; multiple facts may jointly motivate it.
		}
		if ys := yieldsOf[n.ID]; len(ys) > 0 {
			m["yields"] = ys // Downstream facts/findings produced by this intent.
		}
		out = append(out, m)
	}
	return out
}

// ToolSet exposes the PG-backed dual graph (asset + exploration) to an LLM agent.
// One ToolSet is created per planner/worker run; per-run signals live here.
type ToolSet struct {
	language        locale.Lang
	findingRecorder FindingRecorder
	as              *db.AssetStore   // asset store (optional; nil = asset tools not available)
	cs              *db.CompanyStore // company store (optional)
	ts              *db.ExplorationStore
	worker          string
	taskID          int64 // PG tasks.id; 0 when unknown (tests / orchestrator cross-task reads)
	// coverageDisabled mirrors tasks.coverage_enabled=false. Stored inverted so the
	// zero value (all existing ToolSet constructions) means ENABLED — matching the
	// DB default (true). When true: graphOverviewData drops the coverage block, the
	// auto-scope hook (insertAssets) is skipped, and add_task_scope/list_untested_assets
	// are filtered out of the agent's tool list. The scope field stays regardless.
	coverageDisabled bool
	// ownerNode is the exploration node that writes attach to: assets this run
	// touches get anchored to it as lineage/provenance (NOT visibility — the asset
	// graph is global and shared). Worker = its claimed intent; planner = begin root.
	ownerNode int64
	GoalMet   bool
	Reason    string
	writes    WriteCounts
	// killWork, if set, terminates a running work by intent id (engine callback,
	// wired by the planner). nil = the kill_work tool reports unavailable.
	killWork func(intentID int64) error
	// steerWork, if set, queues a mid-run course-correction for the work running an
	// intent id (engine callback, wired by the planner): the worker injects it before
	// its next tool call and re-plans, without being killed. nil = tool unavailable.
	steerWork func(intentID int64, msg string) error
	// enrich, if set, receives async auto-completion triggers (DNS resolve for a
	// domain, HTTP probe for a site). nil = no engine enrichment.
	enrich EnrichTrigger
	// notify, if set, wakes the task's planner after a graph change that should be
	// re-planned promptly (currently: a new hint). nil = no wake (the hint is still
	// stored and read on the next round triggered by other events). debounced.
	notify func()
	// notifyFinding, if set, wakes the task's planner when this run reports a finding,
	// carrying (intentID, summary) so the round can spell out which intent found what.
	// Wired for workers; nil elsewhere → falls back to notify (bare wake).
	notifyFinding func(intentID int64, summary string)
	// resumeTask, if set, revives the task after a graph change that should make a
	// stopped task run again (currently: set_goals adds a goal). It flips a terminal/
	// paused task back to running and (re)starts the engine loops — a plain notify()
	// can't, because the planner's terminal gate swallows wakes. Wired ONLY for the
	// main agent (human steering); nil for the goals decomposer and workers.
	resumeTask func()
	// notifyGoal wakes the planner and records one trigger for the batch of human-added goals.
	// for a whole set_goals call (batch-aware — one call, one trigger, not one per goal)
	// so the next round spells out the added goals (instead of the planner having to
	// spot new open goals in the overview). Wired ONLY for the main agent; nil for the
	// goals decomposer (round-0 has no running planner to inform) and workers → those
	// fall back to the bare notify.
	notifyGoal func(texts []string)
	// notifyHint wakes the planner and records one trigger for the batch of strategic hints.
	// trigger for a whole add_hint call (batch-aware — one call, one trigger) so the next
	// round is told the round was fired by a new hint and spells the hint out, instead of
	// the planner having to spot it folded into the graph overview. Wired for the main
	// agent + cross-task orchestration; nil elsewhere → falls back to the bare notify.
	notifyHint func(texts []string)
}

// SetNotifyGoal wires the goal-add trigger callback (see ToolSet.notifyGoal). Set only
// by the main-agent chat, so runtime-added goals are announced to the planner by name.
func (t *ToolSet) SetNotifyGoal(fn func([]string)) { t.notifyGoal = fn }

// SetNotifyHint wires the hint-add trigger callback (see ToolSet.notifyHint). Set by
// the main-agent chat and cross-task orchestration, so a runtime-added hint fires a
// planner round announced by name instead of a bare wake.
func (t *ToolSet) SetNotifyHint(fn func([]string)) { t.notifyHint = fn }

// SetResumeTask wires the task-revive callback (see ToolSet.resumeTask). Set only by
// the main-agent chat, so runtime-added goals can pull a finished task back to running.
func (t *ToolSet) SetResumeTask(fn func()) { t.resumeTask = fn }

// SetNotify wires the planner-wake callback (see ToolSet.notify). Set by callers
// that hold the task handle (main-agent chat, cross-task orchestration).
func (t *ToolSet) SetNotify(fn func()) { t.notify = fn }

// SetNotifyFinding wires the finding-wake callback (see ToolSet.notifyFinding).
func (t *ToolSet) SetNotifyFinding(fn func(int64, string)) { t.notifyFinding = fn }

// EnrichTrigger is the enrichment engine seen from the tool layer (see package
// enrich). Kept as an interface here to avoid coupling agent → enrich.
type EnrichTrigger interface {
	ResolveDomain(id int64, host string)
	ProbeSite(id int64, url string)
}

// WriteCounts breaks down what a worker persisted this run, by node kind, so the
// engine can log an accurate "wrote back" summary instead of lumping assets and
// findings under "facts" (record_fact → Facts, insert_assets → Assets,
// report_finding → Findings; each element of a batch counts once).
type WriteCounts struct {
	Facts    int
	Assets   int
	Findings int
}

// Total is every node persisted this run, regardless of kind — the
// "explored but persisted nothing" signal (Total == 0).
func (w WriteCounts) Total() int { return w.Facts + w.Assets + w.Findings }

// String renders per-kind counts for logs, such as facts 1, assets 25, findings 0.
func (w WriteCounts) String() string {
	return fmt.Sprintf(locale.Text(locale.ServerDefault(), "facts %d assets %d findings %d"), w.Facts, w.Assets, w.Findings)
}

// Writes reports what this run wrote back, split by node kind (so the engine can
// tell "explored but persisted nothing" apart from a completed intent, and log an
// honest breakdown instead of calling assets/findings "facts").
func (t *ToolSet) Writes() WriteCounts { return t.writes }

func NewToolSet(ts *db.ExplorationStore, worker string, langs ...locale.Lang) *ToolSet {
	lang := locale.En
	if len(langs) > 0 {
		lang = locale.Resolve(langs[0])
	}
	return &ToolSet{ts: ts, worker: worker, language: lang}
}

// SetTaskID sets the PG task id on this ToolSet so that report_finding can
// dual-write to the standalone findings table (which survives task deletion).
func (t *ToolSet) SetTaskID(id int64) { t.taskID = id }

// SetCoverageEnabled records whether this task has the asset-coverage feature on
// (default enabled). Passing false makes graphOverviewData omit the coverage block
// and DropCoverageTools filter the two coverage-only tools out of the agent's tool
// list. It does NOT stop scope accumulation: insertAssets' auto-scope hook runs
// either way, because task_scope is the task's range boundary (the filter basis for
// asset queries), not merely a coverage denominator.
func (t *ToolSet) SetCoverageEnabled(enabled bool) { t.coverageDisabled = !enabled }

// CoverageDisabled reports whether the coverage feature is off for this task.
func (t *ToolSet) CoverageDisabled() bool { return t.coverageDisabled }

// coverageOnlyTools are the LLM tools that only make sense when asset coverage is
// on. When the feature is off they are filtered out of the agent's tool list so
// they neither pollute the prompt nor let the model build a disabled denominator.
// add_task_scope is deliberately NOT here: task_scope is the task's range boundary
// (the filter basis for asset queries), not merely a coverage denominator, so the
// agents responsible for scope definition retain it, matching insertAssets'
// auto-scope hook, which also runs regardless of the switch.
var coverageOnlyTools = map[string]bool{"list_untested_assets": true}

// DropCoverageTools returns tools with the coverage-only ones removed when this
// task has the feature disabled; otherwise it returns tools unchanged.
func (t *ToolSet) DropCoverageTools(tools []actool.CoreTool) []actool.CoreTool {
	if !t.coverageDisabled {
		return tools
	}
	out := tools[:0:0]
	for _, tool := range tools {
		if coverageOnlyTools[tool.Name()] {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// Cross-task reuse: exported accessors returning the per-task tool logic bound to
// THIS ToolSet's store. Host-side orchestration tools build a ToolSet for an
// arbitrary task, then Call these — so cross-task reads/hint reuse the exact
// same logic as the in-task tools. (readTool ignores ToolContext, so Call(…,nil)
// is safe; add_hint is a writeTool but also doesn't deref the context here.)
func (t *ToolSet) GraphOverviewTool() actool.CoreTool      { return t.graphOverview() }
func (t *ToolSet) ListFindingsTool() actool.CoreTool       { return t.listFindings() }
func (t *ToolSet) GetWorkerTraceTool() actool.CoreTool     { return t.getWorkerTrace() }
func (t *ToolSet) ListWorkerTracesTool() actool.CoreTool   { return t.listWorkerTraces() }
func (t *ToolSet) SearchWorkerTracesTool() actool.CoreTool { return t.searchAllWorkerTraces() }
func (t *ToolSet) NodeDetailTool() actool.CoreTool         { return t.nodeDetail() }
func (t *ToolSet) AddHintTool() actool.CoreTool            { return t.addHint() }

// SetEnrich wires the async enrichment engine (DNS/HTTP auto-completion).
func (t *ToolSet) SetEnrich(e EnrichTrigger) { t.enrich = e }

// SetOwnerNode sets the exploration node that writes anchor to (worker: its
// intent node; planner/main: the begin root). Assets created/referenced while
// ownerNode is set are anchored to it as lineage (not visibility).
func (t *ToolSet) SetOwnerNode(id int64) { t.ownerNode = id }

// anchorOwner records a lineage edge from this run's owner node to an asset
// (no-op if unset). Provenance only — the asset graph is global and shared, so
// this no longer affects which assets a task can read.
func (t *ToolSet) anchorOwner(assetID int64) {
	if t.ts != nil && t.ownerNode > 0 && assetID > 0 {
		_ = t.ts.Anchor(t.ownerNode, assetID)
	}
}

// pid parses an id that may arrive as a JSON number or string ("" / 0 → 0).
func pid(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return v
	}
	return 0
}

// pidList parses a list of ids (number|string), dropping zeros/invalids.
func pidList(raw []json.RawMessage) []int64 {
	var out []int64
	for _, r := range raw {
		if v := pid(r); v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func intp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func idp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func readTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:    func(json.RawMessage) bool { return true },
		Concurrent:  func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision { return acperm.Allowed() },
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

func writeTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision { return acperm.Allowed() },
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

// readExpTool / writeExpTool build a domain tool whose handler dereferences the
// task-bound ExplorationStore. Two ToolSets carry a nil store: the catalog's
// seed-only shell (never called) and the server-level one behind buildDomainReg,
// which the tools table can bind to ANY agent — including ones that never run
// within a task (auto/pentest/reporter/custom agents/side questions). Refusing there
// keeps a mis-bound tool a bad tool call; without the guard it was a nil deref,
// and tool handlers run on the harness's own goroutine, so the panic is out of
// reach of every recover() in the server and kills the whole process.
func (t *ToolSet) readExpTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return readTool(name, desc, schema, t.needExploration(name, run))
}

func (t *ToolSet) writeExpTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return writeTool(name, desc, schema, t.needExploration(name, run))
}

// needExploration wraps a handler so it only runs with an exploration store.
// Tools that degrade more usefully than "unavailable" (report_finding points at
// add_task_hint, set_goals/set_constraints at the task itself) keep their own
// bespoke guard instead.
func (t *ToolSet) needExploration(name string, run func(context.Context, json.RawMessage) (actool.Result, error)) func(context.Context, json.RawMessage) (actool.Result, error) {
	return func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		if t.ts == nil {
			return actool.Errorf(name + locale.Text(locale.FromContext(ctx), " requires task context (exploration graph). This agent is not running inside a task, so the tool is unavailable. Use it within a task or use cross-task tools with task_id, such as get_task_node_detail/list_task_findings/get_task_graph.")), nil
		}
		return run(ctx, in)
	}
}

func jsonResult(v any) (actool.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	return actool.Text(string(b)), nil
}

// --- read tools (planner + worker) ---

func (t *ToolSet) graphOverview() actool.CoreTool {
	return t.readExpTool("graph_overview",
		locale.Text(t.language, "(Exploration graph) Distilled situation: asset counts, sites without endpoints, frontier, findings, and hints from the human/main agent. Incorporate hints when creating intents. Read this first when planning."),
		obj(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			return jsonResult(t.graphOverviewData())
		})
}

// graphOverviewData computes the distilled situational snapshot shared by the
// graph_overview tool and the planner's wake-up prompt (which pre-injects it so
// the model needn't spend a turn calling the tool — every plan round starts with
// an empty context and always needs this first).
func (t *ToolSet) graphOverviewData() map[string]any {
	out := map[string]any{}
	// goals summary folded in so the planner needn't call list_goals each round.
	goals, _ := t.ts.ListByKind(db.KindGoal, 100)
	gsum := make([]map[string]any, 0, len(goals))
	for _, g := range goals {
		var p map[string]any
		_ = json.Unmarshal(g.Payload, &p)
		gsum = append(gsum, map[string]any{"id": g.ID, "state": g.State, "text": p["text"]})
	}
	out["goals"] = gsum
	// Hints are human/main-agent strategy notes attached through add_hint. Include
	// them so the planner reads them when generating intents, rather than merely storing them.
	hints, _ := t.ts.ListByKind(db.KindHint, 50)
	hsum := make([]map[string]any, 0, len(hints))
	for _, h := range hints {
		var p map[string]any
		_ = json.Unmarshal(h.Payload, &p)
		hint := map[string]any{"id": h.ID, "state": h.State, "text": p["text"]}
		if findingTrafficBindingEnabled() && p["traffic_refs"] != nil {
			hint["traffic_refs"] = p["traffic_refs"]
		}
		hsum = append(hsum, hint)
	}
	out["hints"] = hsum
	// lineage from the exploration edges: an intent's parents (what it
	// derived_from — possibly several facts combined) and its yields (the
	// facts/findings it produced). factFrom maps a fact → the intent that
	// produced it. This is the relationship layer the flat lists lacked.
	edges, _ := t.ts.Edges(5000)
	parentsOf := map[int64][]int64{}
	yieldsOf := map[int64][]int64{}
	factFrom := map[int64]int64{}
	for _, e := range edges {
		switch e.Rel {
		case db.RelDerivedFrom, db.RelSpawns: // upstream: derived_from (fact/finding/intent→intent) or spawns (origin fact→goal, legacy begin→intent)
			parentsOf[e.To] = append(parentsOf[e.To], e.From)
		case db.RelYields: // intent --yields--> fact/finding
			yieldsOf[e.From] = append(yieldsOf[e.From], e.To)
			factFrom[e.To] = e.From
		}
	}
	// cold-digest §6: members folded into an active digest are shown via cold_digests
	// (below), not the flat recent_* lists. `covered` maps member id → its digest id.
	// §6 render-time revival check: a covered member that has become hot again (a new
	// intent derived from it) must reappear this round — so `hidden` folds a member out
	// only when it is covered AND still cold.
	covered, _ := t.ts.CoveredMembers()
	// Render-time hot set (ancestor of a live intent / fact under a live intent).
	// §6 revival check: a covered member that revived (now hot) must NOT stay folded
	// — hidden() only folds a member out when it is covered AND still cold. Computed
	// every round (cheap for real graph sizes); nil map degrades safely.
	var hotAtRender map[int64]bool
	if cg, _, err := loadColdGraph(t.ts); err == nil {
		hotAtRender = cg.hotSet()
	}
	hidden := func(id int64) bool { _, c := covered[id]; return c && !hotAtRender[id] }
	const openIntentsCap = 30
	fr, _ := t.ts.Frontier(openIntentsCap) // Highest-priority N entries, priority DESC then id ASC; frontier_open holds the full count.
	out["open_intents"] = compactIntents(fr, parentsOf, yieldsOf)
	all, _ := t.ts.ListByKind(db.KindIntent, 300)
	var running, recentDone []*db.Node
	for _, n := range all {
		switch n.State {
		case "running":
			running = append(running, n)
		case "done", "blocked", "exhausted":
			if hidden(n.ID) {
				continue // in a cold_digest and still cold — shown via cold_digests (§6.2)
			}
			recentDone = append(recentDone, n) // Newest first by descending ID; folded entries are removed, then the newest N are emitted.
		}
	}
	out["running_intents"] = compactIntents(running, parentsOf, yieldsOf)
	// done_intents_total counts all completed done/blocked/exhausted intents, while
	// recent_done_intents is only the latest window. Together they expose shown/total
	// so the planner does not mistake omitted intents for never-dispatched directions.
	if dt, err := t.ts.CountFinishedIntents(); err == nil {
		out["done_intents_total"] = dt
	}
	// frontier_open is the full open count; open_intents is its highest-priority truncated window.
	if fo, err := t.ts.CountOpenIntents(); err == nil {
		out["frontier_open"] = fo
	} else {
		out["frontier_open"] = len(fr)
	}
	// findings (confirmed vulns) and facts (worker exploration results) are
	// now distinct node kinds. recent_facts surfaces fact summaries (esp.
	// negative results) so the planner sees them in one call; full content
	// via node_detail(id).
	vulnNodes, _ := t.ts.ListByKind(db.KindFinding, 1000)
	factNodes, _ := t.ts.ListByKind(db.KindFact, 1000) // newest first
	out["findings_total"] = len(vulnNodes)             // Confirmed finding count for goal assessment; finding_list contains the newest details.
	out["facts"] = len(factNodes)                      // Exploration fact/conclusion count, including negative conclusions.
	// Findings are high-value outputs. Include the latest window of at most ten,
	// already newest-first, so each planning round sees recent confirmed findings.
	// Each entry contains id, summary, and optional from_intent, its producing intent.
	// Read evidence/assets/class/severity/state through list_findings or node_detail.
	const findingListCap = 10
	findingList := make([]map[string]any, 0, findingListCap)
	for _, n := range vulnNodes {
		if len(findingList) >= findingListCap {
			break
		}
		var fp map[string]any
		_ = json.Unmarshal(n.Payload, &fp)
		m := map[string]any{"id": n.ID, "summary": fp["summary"]}
		if from := factFrom[n.ID]; from > 0 {
			m["from_intent"] = from // Intent that produced this finding.
		}
		findingList = append(findingList, m)
	}
	out["finding_list"] = findingList
	// recent_facts contains the newest nonfolded fact window. Hidden cold members
	// already represented in cold_digests are not repeated. Entries contain id,
	// summary, optional from_intent/confidence; use node_detail for detail and list_facts for older entries.
	const recentFactsCap = 20
	recentFacts := make([]map[string]any, 0, recentFactsCap)
	for _, n := range factNodes {
		if len(recentFacts) >= recentFactsCap {
			break
		}
		if hidden(n.ID) {
			continue // Still cold and already folded into a digest; see cold_digests.
		}
		m := compactNode(n)
		if from := factFrom[n.ID]; from > 0 {
			m["from_intent"] = from // Intent that produced this fact.
		}
		// Include confidence so inferred conclusions, especially negative ones, are not
		// mistaken for certainties. Longer evidence remains available through node_detail.
		var fp map[string]any
		if json.Unmarshal(n.Payload, &fp) == nil {
			if c, ok := fp["confidence"].(string); ok && c != "" {
				m["confidence"] = c
			}
		}
		recentFacts = append(recentFacts, m)
	}
	out["recent_facts"] = recentFacts
	// recent_done_intents is the latest nonfolded completed-intent window by descending ID.
	// Older entries are represented by done_intents_total and available through node_detail.
	const recentDoneCap = 12
	if len(recentDone) > recentDoneCap {
		recentDone = recentDone[:recentDoneCap]
	}
	out["recent_done_intents"] = compactIntents(recentDone, parentsOf, yieldsOf)
	// Cold-digest section 6.1: newest N digest bodies by member time; expose only IDs
	// for older truncated digests, expandable on demand, to keep cold context bounded.
	const coldDigestsCap = 15
	if cds, more := coldDigestsRecent(t.ts, coldDigestsCap); len(cds) > 0 {
		out["cold_digests"] = cds // Read body directly from {id, body, member_count} entries.
		if len(more) > 0 {
			out["cold_digests_more"] = more // Older truncated digest IDs; expand with expand_digest(id).
		}
	}
	// the original task (root) so the planner always has it, not just the
	// decomposed goals.
	if description, goal, err := t.ts.Root(); err == nil {
		out["task"] = map[string]any{"description": description, "goal": goal}
	}
	// Direct source tasks are a live, read-only blackboard view. Keep their
	// summaries in a separate field so their intents never enter this task's
	// frontier or get mistaken for locally claimable work.
	out["related_tasks"] = t.relatedTaskOverviews()
	// Coverage estimates the share of scoped assets touched by facts, plus per-type
	// totals/tested counts. Agents may inspect list_untested_assets; task context is required.
	// With coverage disabled, retain host_count as target-size context but remove
	// denominator/tested/pct/by_type/note metrics so they do not distract the agent
	// or encourage use of hidden add_task_scope/list_untested_assets tools.
	if t.as != nil && t.ts != nil && t.taskID > 0 {
		{
			m := map[string]any{}
			if !t.coverageDisabled {
				if cov, err := t.as.TaskCoverageWithSources(t.taskID); err == nil {
					m["denominator"] = cov.Denominator
					m["tested"] = cov.Tested
					m["by_type"] = cov.ByType
					m["note"] = locale.Text(t.language, "Approximate asset test coverage, including endpoints and related assets; for reference only. Includes this task's and directly associated tasks' scopes and fact anchors; inherited scopes are read-only. Container assets/large enumerations can depress the estimate: do not assume testing is complete. Use add_task_scope to extend this task's authorized scope and list_untested_assets for untested assets, though normal task progress usually does not require listing them.")
					if cov.Denominator == 0 {
						m["pct"] = nil
						m["status"] = locale.Text(t.language, "Scope is not anchored")
					} else {
						m["pct"] = cov.Pct
					}
				}
			}
			if hosts, err := t.as.HostsByTaskWithSources(t.taskID); err == nil {
				// Return only host count instead of repeating large host lists in every overview;
				// detailed hosts can be queried with list_assets when needed.
				m["host_count"] = len(hosts)
			}
			if len(m) > 0 {
				out["coverage"] = m
			}
		}
	}
	return out
}

func inheritedMap(m map[string]any, sourceTaskID int64) map[string]any {
	m["source_task_id"] = sourceTaskID
	m["inherited"] = true
	return m
}

const (
	relatedOverviewTotalTextRunes      = 48_000
	relatedOverviewMaxTextPerSource    = 8_000
	relatedOverviewMaxGoalsPerSource   = 8
	relatedOverviewMaxHintsPerSource   = 6
	relatedOverviewMaxFactsPerSource   = 12
	relatedOverviewMaxFindingsPerTask  = 6
	relatedOverviewMaxIntentsPerTask   = 8
	relatedOverviewMaxScopePerSource   = 12
	relatedOverviewMaxDigestsPerSource = 6
)

// overviewTextBudget bounds inherited prompt text while preserving a fair slice
// for every direct source. Full evidence remains available through the on-demand
// read tools, so truncation here does not discard persisted blackboard data.
type overviewTextBudget struct {
	remaining int
	truncated bool
}

func relatedOverviewBudgetForSources(sourceCount int) int {
	if sourceCount <= 0 {
		return 0
	}
	if sourceCount > db.MaxTaskSourceCount {
		sourceCount = db.MaxTaskSourceCount
	}
	perSource := relatedOverviewTotalTextRunes / sourceCount
	if perSource > relatedOverviewMaxTextPerSource {
		perSource = relatedOverviewMaxTextPerSource
	}
	return perSource
}

func (b *overviewTextBudget) take(value any, fieldLimit int) string {
	var text string
	switch value := value.(type) {
	case string:
		text = strings.TrimSpace(value)
	case nil:
		return ""
	default:
		text = strings.TrimSpace(fmt.Sprint(value))
	}
	if text == "" {
		return ""
	}
	if b.remaining <= 0 || fieldLimit <= 0 {
		b.truncated = true
		return ""
	}
	runes := []rune(text)
	limit := fieldLimit
	if limit > b.remaining {
		limit = b.remaining
	}
	if len(runes) > limit {
		b.truncated = true
		if limit == 1 {
			text = "…"
		} else {
			text = string(runes[:limit-1]) + "…"
		}
		runes = []rune(text)
	}
	b.remaining -= len(runes)
	return text
}

func recentTerminalIntents(store *db.ExplorationStore, limit int) []*db.Node {
	if limit <= 0 {
		return []*db.Node{}
	}
	const batch = 300
	cursor := int64(0)
	out := make([]*db.Node, 0, limit)
	for len(out) < limit {
		page, more, err := store.ListByKindPage(db.KindIntent, cursor, batch)
		if err != nil || len(page) == 0 {
			break
		}
		for _, intent := range page {
			switch intent.State {
			case "done", "blocked", "exhausted", "stopped":
				out = append(out, intent)
			}
			if len(out) >= limit {
				break
			}
		}
		if !more {
			break
		}
		cursor = page[len(page)-1].ID
	}
	return out
}

// relatedTaskOverviews distills persistent blackboard state from direct source
// tasks. It intentionally reads each source's local store methods, never its own
// related sources, so inheritance is one level only.
func (t *ToolSet) relatedTaskOverviews() []map[string]any {
	sources, err := t.ts.DirectSourceStores()
	if err != nil {
		return []map[string]any{}
	}
	if len(sources) > db.MaxTaskSourceCount {
		sources = sources[:db.MaxTaskSourceCount]
	}
	perSourceTextBudget := relatedOverviewBudgetForSources(len(sources))
	out := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		ts := source.Store
		// §2 cross-task: render the source task's OWN folded view — fold out the
		// members it has already folded, and surface its cold_digests read-only.
		hidden := hiddenMembersFor(ts)
		budget := overviewTextBudget{remaining: perSourceTextBudget}
		item := map[string]any{
			"source_task_id": source.Task.TaskID,
			"inherited":      true,
			"task": map[string]any{
				"description": budget.take(source.Task.Description, 800),
				"goal":        budget.take(source.Task.Goal, 800),
				"status":      source.Task.Status,
			},
		}
		stats, statsErr := ts.Stats()

		edges, _ := ts.Edges(5000)
		parentsOf := map[int64][]int64{}
		yieldsOf := map[int64][]int64{}
		factFrom := map[int64]int64{}
		for _, edge := range edges {
			switch edge.Rel {
			case db.RelDerivedFrom, db.RelSpawns:
				parentsOf[edge.To] = append(parentsOf[edge.To], edge.From)
			case db.RelYields:
				yieldsOf[edge.From] = append(yieldsOf[edge.From], edge.To)
				factFrom[edge.To] = edge.From
			}
		}

		goals, _ := ts.ListByKind(db.KindGoal, relatedOverviewMaxGoalsPerSource)
		goalSummary := make([]map[string]any, 0, len(goals))
		for _, goal := range goals {
			var payload map[string]any
			_ = json.Unmarshal(goal.Payload, &payload)
			goalSummary = append(goalSummary, inheritedMap(map[string]any{
				"id": goal.ID, "state": goal.State, "text": budget.take(payload["text"], 400),
			}, source.Task.TaskID))
		}
		item["goals"] = goalSummary

		hints, _ := ts.ListByKind(db.KindHint, relatedOverviewMaxHintsPerSource)
		hintSummary := make([]map[string]any, 0, len(hints))
		for _, hint := range hints {
			var payload map[string]any
			_ = json.Unmarshal(hint.Payload, &payload)
			hintSummary = append(hintSummary, inheritedMap(map[string]any{
				"id": hint.ID, "state": hint.State, "text": budget.take(payload["text"], 400),
			}, source.Task.TaskID))
		}
		item["hints"] = hintSummary

		facts, _ := ts.ListByKind(db.KindFact, relatedOverviewMaxFactsPerSource)
		findings, _ := ts.ListByKind(db.KindFinding, relatedOverviewMaxFindingsPerTask)
		intentNodes, _ := ts.ListByKind(db.KindIntent, 300)
		terminalIntent := make(map[int64]bool, len(intentNodes))
		for _, intent := range intentNodes {
			terminalIntent[intent.ID] = inheritedIntentSummaryState(intent.State)
		}
		item["facts"] = len(facts)
		item["findings"] = len(findings)
		if statsErr == nil {
			item["facts"] = stats[db.KindFact]
			item["findings"] = stats[db.KindFinding]
			if stats[db.KindGoal] > len(goals) || stats[db.KindHint] > len(hints) ||
				stats[db.KindFact] > len(facts) || stats[db.KindFinding] > len(findings) {
				budget.truncated = true
			}
		}
		recentFindings := make([]map[string]any, 0, len(findings))
		for _, finding := range findings {
			entry := inheritedMap(compactFinding(finding), source.Task.TaskID)
			entry["summary"] = budget.take(entry["summary"], 400)
			recentFindings = append(recentFindings, entry)
		}
		item["recent_findings"] = recentFindings
		recentFacts := make([]map[string]any, 0, len(facts))
		for _, fact := range facts {
			if hidden(fact.ID) {
				continue // folded into this source's cold_digests — shown there (§2/§6.2)
			}
			m := inheritedMap(compactNode(fact), source.Task.TaskID)
			m["summary"] = budget.take(m["summary"], 400)
			if from := factFrom[fact.ID]; from > 0 && terminalIntent[from] {
				m["from_intent"] = from
			}
			var payload map[string]any
			if json.Unmarshal(fact.Payload, &payload) == nil {
				if confidence, ok := payload["confidence"].(string); ok && confidence != "" {
					m["confidence"] = confidence
				}
			}
			recentFacts = append(recentFacts, m)
		}
		item["recent_facts"] = recentFacts

		recentDoneRaw := recentTerminalIntents(ts, relatedOverviewMaxIntentsPerTask)
		recentDone := recentDoneRaw[:0] // in-place filter: drop this source's folded intents (§2)
		for _, intent := range recentDoneRaw {
			if hidden(intent.ID) {
				continue
			}
			recentDone = append(recentDone, intent)
		}
		for _, intent := range recentDone {
			intent.Inherited = true
			intent.SourceTaskID = source.Task.TaskID
		}
		intentResults := compactIntents(recentDone, parentsOf, yieldsOf)
		for i, intent := range recentDone {
			intentResults[i]["summary"] = budget.take(intentResults[i]["summary"], 400)
			acts, _, err := ts.ActivityPageForTerminalIntent(intent.ID, 0, 20)
			if err != nil {
				continue
			}
			var resultSummary, textFallback string
			for _, activity := range acts {
				switch activity.Kind {
				case "result":
					resultSummary = activity.Summary
				case "text":
					textFallback = activity.Summary
				}
			}
			if resultSummary == "" {
				resultSummary = textFallback
			}
			if resultSummary != "" {
				intentResults[i]["result_summary"] = budget.take(resultSummary, 800)
			}
		}
		item["recent_intent_results"] = intentResults
		// §2 cross-task: the source task's folded cold region, read-only, newest-member
		// first & capped like the current task's. Members (and overflow digests) are
		// resolvable via expand_digest(id)/node_detail(id), which search source tasks.
		if cds, more := coldDigestsRecent(ts, relatedOverviewMaxDigestsPerSource); len(cds) > 0 {
			for _, cd := range cds {
				cd["inherited"] = true
				cd["source_task_id"] = source.Task.TaskID
			}
			item["cold_digests"] = cds
			if len(more) > 0 {
				item["cold_digests_more"] = more // Older truncated digest IDs; expand with expand_digest(id).
			}
		}
		if statsErr == nil {
			item["node_stats"] = stats
		}

		if t.as != nil {
			if scopeRows, err := t.as.ListTaskScope(source.Task.TaskID); err == nil && len(scopeRows) > 0 {
				scopeCount := len(scopeRows)
				if len(scopeRows) > relatedOverviewMaxScopePerSource {
					scopeRows = scopeRows[:relatedOverviewMaxScopePerSource]
					budget.truncated = true
				}
				scope := make([]map[string]any, 0, len(scopeRows))
				for _, row := range scopeRows {
					entry := map[string]any{"kind": row.Kind, "source": budget.take(row.Source, 300)}
					switch {
					case row.Domain != "":
						entry["value"] = budget.take(row.Domain, 400)
					case row.Net != "":
						entry["value"] = budget.take(row.Net, 400)
					case row.Value != "":
						entry["value"] = budget.take(row.Value, 400)
					case row.CompanyID != nil:
						entry["company_id"] = *row.CompanyID
					}
					scope = append(scope, entry)
				}
				item["asset_scope"] = scope
				item["asset_scope_count"] = scopeCount
			}
			if coverage, err := t.as.TaskCoverage(source.Task.TaskID, source.Task.ExplorationID); err == nil {
				item["asset_coverage"] = map[string]any{
					"denominator": coverage.Denominator,
					"tested":      coverage.Tested,
					"pct":         coverage.Pct,
					"by_type":     coverage.ByType,
				}
			}
		}
		if budget.truncated {
			item["summary_truncated"] = true
		}
		out = append(out, item)
	}
	return out
}

func inheritedIntentSummaryState(state string) bool {
	switch state {
	case "done", "blocked", "exhausted", "stopped":
		return true
	default:
		return false
	}
}

// compactNode distills any exploration node to id + summary + state, dropping the
// big detail/evidence (fetch that on demand via node_detail).
func compactNode(n *db.Node) map[string]any {
	var p map[string]any
	_ = json.Unmarshal(n.Payload, &p)
	m := map[string]any{"id": n.ID, "state": n.State, "summary": p["summary"]}
	if n.Inherited {
		inheritedMap(m, n.SourceTaskID)
	}
	return m
}

// compactFinding is compactNode plus the vuln-specific vulnclass/severity.
func compactFinding(n *db.Node) map[string]any {
	var p map[string]any
	_ = json.Unmarshal(n.Payload, &p)
	m := map[string]any{"id": n.ID, "state": n.State, "summary": p["summary"]}
	if n.Inherited {
		inheritedMap(m, n.SourceTaskID)
	}
	if vc, ok := p["vulnclass"]; ok && vc != nil && vc != "" {
		m["vulnclass"] = vc
	}
	if sv, ok := p["severity"]; ok && sv != nil && sv != "" {
		m["severity"] = sv
	}
	return m
}

func (t *ToolSet) listFindings() actool.CoreTool {
	return t.readExpTool("list_findings", locale.Text(t.language, "List confirmed vulnerabilities in this task and directly associated tasks (compact: id/task_id/intent_id/vulnclass/severity/summary/status). Inherited entries have source_task_id/inherited=true and are read-only. Ordinary exploration facts are in list_facts; use node_detail(id) for full details."),
		obj(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			f, _ := t.ts.ListByKindWithSources(db.KindFinding, 500)
			if err := t.ts.PopulateFindingTrafficIDs(f); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			intentOf, _ := t.ts.FindingIntentsWithSources() // Finding ID to producing intent ID.
			taskID := t.taskID
			if taskID <= 0 {
				taskID, _ = t.ts.TaskID()
			}
			out := make([]map[string]any, 0, len(f))
			for _, n := range f {
				m := compactFinding(n)
				if n.FindingID > 0 {
					m["finding_id"], m["finding_node_id"], m["traffic_count"] = n.FindingID, n.ID, n.TrafficCount
				}
				if n.Inherited {
					m["task_id"] = n.SourceTaskID
				} else {
					m["task_id"] = taskID
				}
				if iid, ok := intentOf[n.ID]; ok {
					m["intent_id"] = iid
				}
				out = append(out, m)
			}
			return jsonResult(out)
		})
}

// factsPageSize is the default page size for list_facts. Facts pile up on long
// tasks; returning all of them at once (the old behaviour) could blow up the
// context, so default to the newest page and let the agent page/filter for more.
const factsPageSize = 20

func (t *ToolSet) listFacts() actool.CoreTool {
	return t.readExpTool("list_facts", locale.Text(t.language, "Paginate exploration facts/conclusions from this task and directly associated tasks, newest first (id/summary/state; long summaries are truncated, use node_detail(id) for full text). Optional limit defaults to 20, maximum 100. before uses the previous next_before cursor for older facts; omitted/0 starts at newest. q filters summary keywords. Returns {facts,total,has_more,next_before}; total is filtered count. Inherited entries have source_task_id/inherited=true and are read-only. Use list_findings for vulnerabilities."),
		obj(map[string]any{
			"limit":  intp(locale.Text(t.language, "Result count, default 20, maximum 100")),
			"before": intp(locale.Text(t.language, "Pagination cursor: return facts with smaller IDs; omitted/0 means newest page")),
			"q":      str(locale.Text(t.language, "Case-insensitive summary keyword filter; omitted means no filter")),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Limit  int    `json:"limit"`
				Before int64  `json:"before"`
				Q      string `json:"q"`
			}
			_ = json.Unmarshal(in, &a)
			limit := a.Limit
			if limit <= 0 {
				limit = factsPageSize
			}
			if limit > 100 {
				limit = 100
			}
			f, hasMore, total, err := t.ts.ListByKindPageWithSources(db.KindFact, a.Before, limit, strings.TrimSpace(a.Q))
			if err != nil {
				return actool.Result{}, err
			}
			out := make([]map[string]any, 0, len(f))
			for _, n := range f {
				out = append(out, compactFact(n))
			}
			res := map[string]any{"facts": out, "total": total, "has_more": hasMore}
			if hasMore && len(f) > 0 {
				res["next_before"] = f[len(f)-1].ID // Pass this cursor to retrieve the next, older page.
			}
			return jsonResult(res)
		})
}

// factSummaryMax caps a fact summary in list_facts output. Facts carry one-line
// conclusions, but nothing enforces brevity; a runaway summary must not bloat a
// whole page. Full text stays available via node_detail(id).
const factSummaryMax = 160

// compactFact is compactNode with the summary rune-capped for list_facts, so a
// page of facts stays bounded regardless of how long any single summary grew.
func compactFact(n *db.Node) map[string]any {
	m := compactNode(n)
	if s, ok := m["summary"].(string); ok && len([]rune(s)) > factSummaryMax {
		m["summary"] = string([]rune(s)[:factSummaryMax]) + "…"
		m["summary_truncated"] = true
	}
	return m
}

func (t *ToolSet) nodeDetail() actool.CoreTool {
	return t.readExpTool("node_detail", locale.Text(t.language, "Get an exploration node's full content by ID from this task or a directly associated task. Inherited nodes have source_task_id/inherited=true and are read-only. Only pass exploration IDs from list_facts/list_findings/graph_overview. Use list_assets/asset_neighbors for assets."),
		obj(map[string]any{"id": idp(locale.Text(t.language, "Exploration node ID, not an asset ID"))}, "id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(in, &a)
			id := pid(a.ID)
			if id <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "id is required")), nil
			}
			n, err := t.ts.GetNodeWithSources(id)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if n == nil {
				return actool.Errorf(fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Exploration node %d was not found. For assets use list_assets/asset_neighbors: asset IDs and exploration node IDs are separate namespaces, and node_detail does not accept asset IDs."), id)), nil
			}
			if err := t.ts.PopulateFindingTrafficIDs([]*db.Node{n}); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return jsonResult(n) // full payload incl. detail / evidence, plus explicit finding IDs
		})
}

// --- planner write tools ---

// intentItem represents one exploration direction in single or batch add_intent calls.
type intentItem struct {
	Summary   string            `json:"summary"`
	AssetIDs  []json.RawMessage `json:"asset_ids"`
	ParentIDs []json.RawMessage `json:"parent_ids"`
	Priority  int               `json:"priority"`
}

// addOneIntent creates an intent, links upstream lineage, and returns its ID.
// Intents may anchor only to existing confirmed fact/finding nodes, never
// intents/goals/hints. New top-level directions use empty parent_ids and link to origin.
// Enforce fact-anchored, finding-driven planning directly in the creation path.
func (t *ToolSet) addOneIntent(it intentItem) (int64, error) {
	if strings.TrimSpace(it.Summary) == "" {
		return 0, locale.Errorf("summary cannot be empty")
	}
	// Validate anchors before node creation to avoid leaving orphan intents.
	parents := pidList(it.ParentIDs)
	for _, pidv := range parents {
		n, err := t.ts.GetNodeWithSources(pidv)
		if err != nil || n == nil {
			return 0, locale.Errorf("parent_id %d does not exist in this task or directly associated tasks. parent_ids must reference existing fact/finding nodes; leave it empty for a new top-level direction.", pidv)
		}
		if n.Kind != db.KindFact && n.Kind != db.KindFinding {
			return 0, locale.Errorf("parent_id %d is a %q node and cannot anchor an intent. Only existing fact/finding nodes may anchor intents, not intents/goals/hints. Leave parent_ids empty for a new top-level direction.", pidv, n.Kind)
		}
	}
	priority := it.Priority
	if priority == 0 {
		priority = 5
	}
	anchors := pidList(it.AssetIDs)
	// Reject intents whose bound assets match platform asset-interception rules.
	if t.as != nil && len(anchors) > 0 {
		hits, err := t.as.CheckAssetsInterceptForLanguage(t.language, t.taskID, anchors)
		if err != nil {
			return 0, locale.Errorf("Asset interception validation failed: %w", err)
		}
		if len(hits) > 0 {
			var b strings.Builder
			fmt.Fprintf(&b, locale.Text(t.language, "Assets bound to intent \"%s\" failed scope validation. Stop testing the related assets: "), it.Summary)
			for _, h := range hits {
				fmt.Fprintf(&b, "\n - %s", h.DescribeForLanguage(t.language))
			}
			return 0, fmt.Errorf("%s", b.String())
		}
	}
	payload := map[string]any{"summary": it.Summary}
	if len(anchors) > 0 {
		payload["asset_ids"] = anchors
	}
	id, err := t.ts.AddIntent(payload, priority, anchors, "planner")
	if err != nil {
		return 0, err
	}
	// upstream lineage: link each (validated) fact/finding parent → this intent, so
	// "multiple facts combine into one new intent" is expressible.
	for _, parent := range parents {
		_ = t.ts.Link(parent, db.RelDerivedFrom, id)
	}
	// a top-level intent (no explicit parent) connects to the origin fact, so every
	// intent still traces back to a fact node — at task start the only fact is the
	// origin, and the first intents derive from it.
	if len(parents) == 0 {
		if origin, _ := t.ts.OriginFactID(); origin > 0 {
			_ = t.ts.Link(origin, db.RelDerivedFrom, id)
		}
	}
	return id, nil
}

func (t *ToolSet) addIntent() actool.CoreTool {
	return t.writeExpTool("add_intent", locale.Text(t.language, "Create exploration directions in the frontier and link them into the exploration graph. Intents are open-ended: describe what to explore/verify/exploit in one summary sentence.\n")+
		locale.Text(t.language, "Prefer batching: submit this round's new directions together in intents. Returned ids match input length/order; failed items have id=0 and details in errors. For one item, omit intents and provide top-level summary."),
		obj(map[string]any{
			"intents":    map[string]any{"type": "array", "description": locale.Text(t.language, "Preferred: new directions processed in order. Each item uses summary/asset_ids/parent_ids/priority. Returned ids match input length/order."), "items": map[string]any{"type": "object"}},
			"summary":    str(locale.Text(t.language, "Single item: one sentence describing what to do and why. A clear direction does not require an asset ID.")),
			"asset_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Target asset IDs from list_assets, not exploration node IDs (zero/one/many). Include IDs whenever the direction targets concrete sites/endpoints/parameters/hosts: they identify targets, support coverage deduplication, and connect asset lineage. Leave empty only for global reconnaissance with no concrete asset.")},
			"parent_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Optional upstream anchors (zero/one/many): existing confirmed fact/finding node IDs supporting this direction. Never use intent/goal/hint IDs. Include all combined facts. Leave empty for new top-level reconnaissance; it attaches automatically to the task's origin fact.")},
			"priority":   intp(locale.Text(t.language, "Priority 0-10, default 5")),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Intents    []intentItem `json:"intents"`
				intentItem              // Single-item mode uses top-level summary/asset_ids/parent_ids/priority.
			}
			_ = json.Unmarshal(in, &a)
			batch := len(a.Intents) > 0
			items := a.Intents
			if !batch {
				items = []intentItem{a.intentItem}
			}

			ids := make([]int64, len(items))
			errs := map[string]string{}
			createdAny := false
			for i, it := range items {
				id, err := t.addOneIntent(it)
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				ids[i] = id
				createdAny = true
			}

			// A human-injected intent revives a completed/goalless task so a worker can claim
			// it. Only main-agent Chat wires resumeTask through SetResumeTask; planners leave
			// it nil, so their own add_intent calls do not change ordinary planning behavior.
			// The open intent already exists before revival, preventing a false drained-task judgment.
			if createdAny && t.resumeTask != nil {
				t.resumeTask()
			}

			if !batch { // Single-item mode retains the original response.
				if e, bad := errs["0"]; bad {
					return actool.Errorf(e), nil
				}
				return actool.Text(fmt.Sprintf("intent created: %d", ids[0])), nil
			}
			out := map[string]any{"ids": ids}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		})
}

func (t *ToolSet) listGoals() actool.CoreTool {
	return t.readExpTool("list_goals", locale.Text(t.language, "List this task's goal nodes and open/met states to assess achievement."),
		obj(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			g, _ := t.ts.ListByKind(db.KindGoal, 100)
			return jsonResult(g)
		})
}

func (t *ToolSet) proveGoal() actool.CoreTool {
	return t.writeExpTool("prove_goal", locale.Text(t.language, "Call when a finding/fact proves an objective: link the evidence node to the goal and mark it met."),
		obj(map[string]any{
			"goal_id":     idp(locale.Text(t.language, "Goal node ID")),
			"evidence_id": idp(locale.Text(t.language, "Finding/fact node ID proving the goal")),
			"reason":      str(locale.Text(t.language, "Why this evidence satisfies the objective")),
		}, "goal_id", "evidence_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				GoalID     json.RawMessage `json:"goal_id"`
				EvidenceID json.RawMessage `json:"evidence_id"`
				Reason     string          `json:"reason"`
			}
			_ = json.Unmarshal(in, &a)
			goal, ev := pid(a.GoalID), pid(a.EvidenceID)
			if goal == 0 || ev == 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "goal_id and evidence_id are required")), nil
			}
			goalNode, err := t.ts.GetNode(goal)
			if err != nil || goalNode == nil || goalNode.Kind != db.KindGoal {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "goal_id must be a goal in this task; associated-task goals are read-only")), nil
			}
			evidenceNode, err := t.ts.GetNodeWithSources(ev)
			if err != nil || evidenceNode == nil || (evidenceNode.Kind != db.KindFact && evidenceNode.Kind != db.KindFinding) {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "evidence_id must be a fact/finding in this task or a directly associated task")), nil
			}
			_ = t.ts.Link(ev, db.RelProves, goal)
			_ = t.ts.SetNodeState(goal, "met")
			// After each prove_goal, check whether all goals are met and set GoalMet
			// automatically, without requiring an explicit goal_met call from the model.
			if goals, err := t.ts.ListByKind(db.KindGoal, 1000); err == nil && len(goals) > 0 {
				allMet := true
				for _, g := range goals {
					if g.State != "met" {
						allMet = false
						break
					}
				}
				if allMet {
					t.GoalMet = true
					t.Reason = fmt.Sprintf(locale.Text(locale.FromContext(ctx), "All %d goals are met (last triggered by goal %d)"), len(goals), goal)
					return actool.Text(fmt.Sprintf(locale.Text(locale.FromContext(ctx), "goal %d marked met; all task objectives are achieved and the task completes automatically"), goal)), nil
				}
			}
			return actool.Text(fmt.Sprintf("goal %d marked met", goal)), nil
		})
}

func (t *ToolSet) goalMet() actool.CoreTool {
	return writeTool("goal_met", locale.Text(t.language, "End the ENTIRE task immediately only when ALL objectives are truly achieved. One objective/flag/vulnerability alone is insufficient; use prove_goal for that goal instead. This is NOT for ending a planning round: no new intents or waiting for workers means simply end the round without calling this tool; zero intents can be normal. Prefer proving goals individually with prove_goal. goal_met bypasses individual proof to close the whole engagement."),
		obj(map[string]any{"reason": str(locale.Text(t.language, "Evidence that the objective was genuinely achieved, not a reason to end this round such as no new directions"))}, "reason"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct{ Reason string }
			_ = json.Unmarshal(in, &a)
			t.GoalMet = true
			t.Reason = a.Reason
			return actool.Text("acknowledged: goal marked met"), nil
		})
}

// --- worker write tools ---

func (t *ToolSet) addFinding() actool.CoreTool {
	return writeTool("report_finding", locale.Text(t.language, "Record a confirmed vulnerability with verifiable command output/logs in evidence. In task context supply the current intent_id. Returned finding_id is the independent finding record ID; finding_node_id is the exploration node ID, retained on the first output line."), obj(map[string]any{
		"vulnclass": str(locale.Text(t.language, "Vulnerability class")), "name": str(locale.Text(t.language, "Vulnerability name")), "severity": str("critical|high|medium|low"), "summary": str(locale.Text(t.language, "Finding summary")),
		"intent_id": idp(locale.Text(t.language, "Intent ID in the current task")), "asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Affected asset ID")},
		"evidence":         str(locale.Text(t.language, "Evidence/PoC text")),
		"evidence_hint_id": idp(locale.Text(t.language, "Optional hint node ID in this task for this finding; includes its structured traffic_refs. Do not reference inherited hints or another finding's hint.")),
		"traffic_refs": map[string]any{"type": "array", "description": locale.Text(t.language, "Optional: for HTTP/HTTPS, search and verify each request/response supports the finding before supplying actual IDs in reproduction order. For TCP/non-HTTP, absent capture, or no exact match, omit or pass []; reporting remains allowed. Explain the absence and supply other verifiable evidence. Never guess IDs, infer association from domain/time alone, or repeat probes merely for capture. Roles: baseline normal control, proof vulnerability evidence, verification additional validation, supporting supplementary evidence."),
			"items": obj(map[string]any{"traffic_id": str(locale.Text(t.language, "Actual traffic ID returned by traffic_search")), "role": map[string]any{"type": "string", "enum": []string{"baseline", "proof", "verification", "supporting"}}, "note": str(locale.Text(t.language, "How this traffic supports the finding"))}, "traffic_id")},
	}, "vulnclass", "severity", "summary"), func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		var a struct {
			VulnClass, Name, Severity, Summary, Evidence string
			IntentID                                     json.RawMessage   `json:"intent_id"`
			AssetIDs                                     []json.RawMessage `json:"asset_ids"`
			TrafficRefs                                  []db.TrafficRef   `json:"traffic_refs"`
			EvidenceHintID                               json.RawMessage   `json:"evidence_hint_id"`
		}
		if err := json.Unmarshal(in, &a); err != nil {
			return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
		}
		if t.ts == nil {
			return actool.Errorf(locale.Text(locale.FromContext(ctx), "report_finding requires task context. In platform chat, hand the finding and existing traffic_refs to the relevant task using add_task_hint for its agent to register. Existing findings can receive additional bindings through bind_finding_traffic.")), nil
		}
		// Auto-binding off: ignore the evidence params instead of rejecting the call.
		// stripTrafficParameters already removes them from the advertised schema, but
		// models routinely emit fields anyway — failing here would discard a confirmed
		// finding over a stray parameter. The success path below reports evidence_status
		// not_bound with a disabled/manual-linking note gives the caller actionable status.
		if !findingTrafficBindingEnabled() {
			a.TrafficRefs, a.EvidenceHintID = nil, nil
		}
		if len(a.EvidenceHintID) > 0 && pid(a.EvidenceHintID) <= 0 {
			return actool.Errorf(locale.Text(locale.FromContext(ctx), "evidence_hint_id must be a valid hint node ID; omit it when no handoff hint exists")), nil
		}
		refs, err := t.findingRefsFromHint(pid(a.EvidenceHintID), a.TrafficRefs)
		if err != nil {
			return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
		}
		input := db.RecordFindingInput{TaskID: t.taskID, ExplorationID: t.ts.ID(), IntentID: pid(a.IntentID), VulnClass: a.VulnClass, Name: a.Name, Severity: a.Severity, Summary: a.Summary, Evidence: a.Evidence, Worker: t.worker, AssetIDs: pidList(a.AssetIDs)}
		var recorded *db.RecordedFinding
		if t.findingRecorder != nil {
			recorded, err = t.findingRecorder.Record(ctx, input, refs)
		} else if len(refs) > 0 {
			return actool.Errorf(locale.Text(locale.FromContext(ctx), "Traffic evidence storage unavailable; finding was not recorded")), nil
		} else {
			recorded, err = t.ts.RecordFinding(ctx, input)
		}
		if err != nil {
			return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
		}
		if t.notifyFinding != nil {
			iid := input.IntentID
			if iid <= 0 {
				iid = t.ownerNode
			}
			t.notifyFinding(iid, a.Summary)
		} else if t.notify != nil {
			t.notify()
		}
		t.writes.Findings++
		// Keep the first line's node-ID contract for existing reporter triggers.
		for i := range recorded.Traffic.Bindings {
			recorded.Traffic.Bindings[i].Snapshot.ReqHead = ""
			recorded.Traffic.Bindings[i].Snapshot.RespHead = ""
		}
		result := struct {
			*db.RecordedFinding
			EvidenceStatus string `json:"evidence_status"`
			EvidenceNote   string `json:"evidence_note,omitempty"`
		}{RecordedFinding: recorded, EvidenceStatus: "bound"}
		if len(recorded.Traffic.Bindings) == 0 {
			result.EvidenceStatus = "not_bound"
			result.EvidenceNote = locale.Text(locale.FromContext(ctx), "Finding saved without traffic bindings. TCP/no-packet cases may continue normally. If verified HTTP traffic exists, bind it using bind_finding_traffic or the finding page before completing handoff. Do not create a duplicate finding.")
			if !findingTrafficBindingEnabled() {
				result.EvidenceNote = locale.Text(locale.FromContext(ctx), "Finding saved. Automatic agent traffic binding is disabled; traffic can be linked manually on the page.")
			}
		}
		raw, _ := json.Marshal(result)
		return actool.Text(fmt.Sprintf("finding recorded: %d\n%s", recorded.NodeID, raw)), nil
	})
}

// recordFact writes a general exploration RESULT/conclusion (not a vuln, not a
// new asset) into the EXPLORATION graph, chained to the intent that produced it.
// This is the home for observations and — importantly — negative results
// ("port closed", "param not injectable", "no login found"). Such conclusions
// must NOT be stuffed into the asset graph via upsert_asset.
// factItem represents one fact in single or batch record_fact calls.
type factItem struct {
	Summary    string            `json:"summary"`
	Detail     string            `json:"detail"`
	Evidence   string            `json:"evidence"`   // One concise evidence line: command plus key output, supporting later verification.
	Confidence string            `json:"confidence"` // observed for direct observation, inferred for deductions.
	IntentID   json.RawMessage   `json:"intent_id"`
	AssetIDs   []json.RawMessage `json:"asset_ids"`
}

// recordOneFact writes a fact linked by intent -> yields -> fact. defaultIntent
// supplies the batch-level intent when an item omits intent_id.
func (t *ToolSet) recordOneFact(it factItem, defaultIntent int64) (int64, error) {
	if strings.TrimSpace(it.Summary) == "" {
		return 0, locale.Errorf("summary cannot be empty")
	}
	payload := map[string]any{"summary": it.Summary}
	if it.Detail != "" {
		payload["detail"] = it.Detail
	}
	if e := strings.TrimSpace(it.Evidence); e != "" {
		payload["evidence"] = e
	}
	if c := strings.TrimSpace(it.Confidence); c != "" {
		payload["confidence"] = c
	}
	intent := pid(it.IntentID)
	if intent <= 0 {
		intent = defaultIntent
	}
	if intent > 0 {
		node, err := t.ts.GetNode(intent)
		if err != nil || node == nil || node.Kind != db.KindIntent {
			return 0, locale.Errorf("intent_id must belong to this task; associated-task intents are read-only")
		}
	}
	// a fact is its OWN node kind (distinct from a vuln finding).
	id, err := t.ts.AddNode(db.KindFact, payload, 5, "confirmed", t.worker, pidList(it.AssetIDs))
	if err != nil {
		return 0, err
	}
	if intent > 0 {
		_ = t.ts.Link(intent, db.RelYields, id) // chain: intent -> fact
	}
	t.writes.Facts++
	return id, nil
}

func (t *ToolSet) recordFact() actool.CoreTool {
	return t.writeExpTool("record_fact", locale.Text(t.language, "Write exploration facts/conclusions to the exploration graph and link them to the producing intent_id. Record positive results such as fingerprints/enumeration and negative observations such as closed ports, non-injectable parameters, or no login entry found.\n")+
		locale.Text(t.language, "Consolidate one exploration's observations into ONE fact whenever possible. summary is a one-sentence conclusion; detail contains related specifics. For example, record one fact describing the site's stack and response features, with nginx/Vue/status/title/body length in detail, rather than separate facts for each property. One intent normally yields one fact; excessive splitting bloats the graph.\n")+
		locale.Text(t.language, "Use the facts array for genuinely different conclusions in one call. Each may omit intent_id to use the top-level default. Returned ids match input length/order.\n")+
		locale.Text(t.language, "Record only conclusions actually observed in tool output; never invent them. evidence and confidence prevent unsupported conclusions from polluting the graph:\n")+
		locale.Text(t.language, "  - evidence: ONE concise line containing the command and the one or two strongest output lines. Keep details in detail; do not paste large output again.\n")+
		locale.Text(t.language, "  - confidence: observed (directly seen in output) or inferred (deduced from observations).\n")+
		locale.Text(t.language, "  - Negative conclusions should state actual observations plus a tentative interpretation; the planner decides whether to abandon the direction using the full situation. Always provide evidence. Incomplete methods or weak evidence (one attempt, apparent similarity) require inferred; use observed only when methods are exhausted and the result is directly seen."),
		obj(map[string]any{
			"facts":      map[string]any{"type": "array", "description": locale.Text(t.language, "Use for multiple distinct conclusions. Each fact has summary/detail/evidence/confidence/intent_id/asset_ids; omitted intent_id uses the top-level value. Returned ids match input length/order."), "items": map[string]any{"type": "object"}},
			"summary":    str(locale.Text(t.language, "One-sentence conclusion summarizing detail")),
			"intent_id":  idp(locale.Text(t.language, "Producing intent ID: your assigned intent, used as the default for batch items")),
			"detail":     str(locale.Text(t.language, "Related details: consolidate this exploration's observations here")),
			"evidence":   str(locale.Text(t.language, "ONE concise evidence line: command plus the one or two strongest output lines. Put large output/details in detail.")),
			"confidence": str(locale.Text(t.language, "observed means directly seen in output; inferred means deduced. Label negative conclusions honestly.")),
			"asset_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Optional related asset IDs (zero/one/many)")},
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Facts    []factItem `json:"facts"`
				factItem            // Single-item fields plus the default batch intent_id.
			}
			_ = json.Unmarshal(in, &a)
			batch := len(a.Facts) > 0
			items := a.Facts
			if !batch {
				items = []factItem{a.factItem}
			}
			defaultIntent := pid(a.factItem.IntentID) // Top-level intent_id is the batch default.

			ids := make([]int64, len(items))
			errs := map[string]string{}
			for i, it := range items {
				id, err := t.recordOneFact(it, defaultIntent)
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				ids[i] = id
			}

			if !batch { // Single-item mode retains the original response.
				if e, bad := errs["0"]; bad {
					return actool.Errorf(e), nil
				}
				return actool.Text(fmt.Sprintf("fact recorded: %d", ids[0])), nil
			}
			out := map[string]any{"ids": ids}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		})
}

type hintItem struct {
	Text        string            `json:"text"`
	AssetIDs    []json.RawMessage `json:"asset_ids"`
	TrafficRefs []db.TrafficRef   `json:"traffic_refs"`
}

// addOneHint attaches an active/human hint, optionally anchors assets, and returns its ID.
func (t *ToolSet) addOneHint(it hintItem) (int64, error) {
	if len(it.TrafficRefs) > 0 && !findingTrafficBindingEnabled() {
		return 0, locale.Errorf("Automatic agent traffic binding is disabled; the hint with traffic_refs was not saved. Enable it in settings or hand off text only.")
	}
	if strings.TrimSpace(it.Text) == "" {
		return 0, locale.Errorf("text cannot be empty")
	}
	var anchors []int64
	for _, raw := range it.AssetIDs {
		if tid := pid(raw); tid > 0 {
			anchors = append(anchors, tid)
		}
	}
	refs, err := db.NormalizeTrafficRefs(it.TrafficRefs)
	if err != nil {
		return 0, err
	}
	payload := map[string]any{"text": it.Text}
	if len(refs) > 0 {
		payload["traffic_refs"] = refs
	}
	// addHint wakes the planner once after the entire batch is written, carrying all
	// hint text, rather than flooding planner triggers once per individual hint.
	return t.ts.AddNode(db.KindHint, payload, 0, "active", "human", anchors)
}

type goalItem struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass"`
}

// addOneGoal creates an open goal linked from the task's origin fact through spawns.
// Origin uses t.worker, defaulting to system: goals for decomposition, human for
// the main agent. setGoals performs one post-batch wake; this helper only persists.
func (t *ToolSet) addOneGoal(it goalItem) (int64, error) {
	text := strings.TrimSpace(it.Text)
	if text == "" {
		return 0, locale.Errorf("text cannot be empty")
	}
	payload := map[string]any{"text": text}
	if vc := strings.TrimSpace(it.VulnClass); vc != "" {
		payload["vulnclass"] = vc
	}
	origin := t.worker
	if origin == "" {
		origin = "system"
	}
	id, err := t.ts.AddNode(db.KindGoal, payload, 0, "open", origin, nil)
	if err != nil {
		return 0, err
	}
	if of, _ := t.ts.OriginFactID(); of > 0 && id > 0 {
		_ = t.ts.Link(of, db.RelSpawns, id) // goals descend from the task root (origin fact)
	}
	return id, nil
}

// setGoals adds task goals for both round-zero decomposition and runtime human
// steering. It is one managed tool with UI-editable descriptions/schema and bindings.
func (t *ToolSet) setGoals() actool.CoreTool {
	return writeTool("set_goals",
		locale.Text(t.language, "Add exploration goals to THIS task. A goal is a final deliverable/verifiable outcome, not an attack step or reconnaissance action.\n")+
			locale.Text(t.language, "Prefer a single batch in goals. Returned ids match its length/order; failures have id=0 with details in errors. For one goal, omit goals and provide top-level text.\n")+
			locale.Text(t.language, "Optional vulnclass identifies a vulnerability class such as SQLi/IDOR; leave empty for business-logic goals. The system marks achievement as met; this tool only adds goals."),
		obj(map[string]any{
			"goals":     map[string]any{"type": "array", "description": locale.Text(t.language, "Preferred: ordered goals array. Each item has required text (one independent verifiable final objective) and optional vulnclass. Returned ids match length/order."), "items": map[string]any{"type": "object"}},
			"text":      str(locale.Text(t.language, "Single item: one independent verifiable final objective")),
			"vulnclass": str(locale.Text(t.language, "Single item: specific vulnerability class such as SQLi/IDOR; empty for business-logic goals")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.ts == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "set_goals unavailable: ExplorationStore is not initialized")), nil
			}
			var a struct {
				Goals    []goalItem `json:"goals"`
				goalItem            // Single-item mode uses top-level text/vulnclass.
			}
			_ = json.Unmarshal(in, &a)
			batch := len(a.Goals) > 0
			items := a.Goals
			if !batch {
				items = []goalItem{a.goalItem}
			}

			ids := make([]int64, len(items))
			errs := map[string]string{}
			var addedTexts []string
			for i, it := range items {
				id, err := t.addOneGoal(it)
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				ids[i] = id
				addedTexts = append(addedTexts, strings.TrimSpace(it.Text))
			}
			if len(addedTexts) > 0 {
				// Wake once per batch, preferring notifyGoal so one set_goals call records one
				// human-added-goals trigger. Decomposer/worker fall back to notify when available;
				// round-zero decomposition has neither callback because the planner has not started.
				switch {
				case t.notifyGoal != nil:
					t.notifyGoal(addedTexts)
				case t.notify != nil:
					t.notify()
				}
				// Runtime human goals must explicitly revive completed/paused tasks; ordinary
				// notify is swallowed by terminal gates. Only mainagent wires this callback.
				if t.resumeTask != nil {
					t.resumeTask()
				}
			}

			if !batch { // Single-item mode retains the original response.
				if e, bad := errs["0"]; bad {
					return actool.Errorf(e), nil
				}
				return actool.Text(fmt.Sprintf("goal added: %d", ids[0])), nil
			}
			out := map[string]any{"ids": ids}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		})
}

type constraintItem struct {
	Text string `json:"text"`
	Type string `json:"type"` // allow | deny
}

// addOneConstraint writes task_constraints with origin t.worker (default system):
// goals for the decomposer and human for the main agent.
func (t *ToolSet) addOneConstraint(it constraintItem) (int64, error) {
	text := strings.TrimSpace(it.Text)
	if text == "" {
		return 0, locale.Errorf("text cannot be empty")
	}
	kind := strings.TrimSpace(strings.ToLower(it.Type))
	if kind == "" {
		kind = "deny" // Default to deny when unspecified, as the conservative interpretation.
	}
	if kind != "allow" && kind != "deny" {
		return 0, locale.Errorf("type must be allow or deny")
	}
	return t.ts.AddConstraint(kind, text, t.worker)
}

// setConstraints adds allow/deny task boundaries for both round-zero extraction
// and runtime human steering. The managed tool supports UI metadata and bindings;
// constraints are injected into planner/worker system prompts to bound exploration.
func (t *ToolSet) setConstraints() actool.CoreTool {
	return writeTool("set_constraints",
		locale.Text(t.language, "Add operating constraints to THIS task: type=allow for permitted operations or deny for prohibited operations.\n")+
			locale.Text(t.language, "Constraints define what may/may not be done (only the target port, no scanning other ports, no production database writes, passive reconnaissance only). They are not objectives or attack steps.\n")+
			locale.Text(t.language, "Prefer one constraints batch. Returned ids match input length/order; failures have id=0 with details in errors. For one item, omit constraints and provide top-level text/type.\n")+
			locale.Text(t.language, "Register only constraints explicitly stated in the task objective/description. Do not invent them; use deny conservatively if the type is uncertain."),
		obj(map[string]any{
			"constraints": map[string]any{"type": "array", "description": locale.Text(t.language, "Preferred: ordered constraints array. Each item has required text and type (allow or deny). Returned ids match length/order."), "items": map[string]any{"type": "object"}},
			"text":        str(locale.Text(t.language, "Single item: the operating constraint text")),
			"type":        str(locale.Text(t.language, "Single item: allow or deny; defaults to deny")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.ts == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "set_constraints unavailable: ExplorationStore is not initialized")), nil
			}
			var a struct {
				Constraints    []constraintItem `json:"constraints"`
				constraintItem                  // Single-item mode uses top-level text/type.
			}
			_ = json.Unmarshal(in, &a)
			batch := len(a.Constraints) > 0
			items := a.Constraints
			if !batch {
				items = []constraintItem{a.constraintItem}
			}
			ids := make([]int64, len(items))
			errs := map[string]string{}
			for i, it := range items {
				id, err := t.addOneConstraint(it)
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				ids[i] = id
			}
			if !batch { // Single-item mode retains a simple response.
				if e, bad := errs["0"]; bad {
					return actool.Errorf(e), nil
				}
				return actool.Text(fmt.Sprintf("constraint added: %d", ids[0])), nil
			}
			out := map[string]any{"ids": ids}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		})
}

func (t *ToolSet) addHint() actool.CoreTool {
	return t.writeExpTool("add_hint", locale.Text(t.language, "Attach a strategic hint from the human/main agent to the exploration graph for the planner's next intent-generation round.\n")+
		locale.Text(t.language, "Prefer one hints batch to reduce round trips. Returned ids match length/order; failures have id=0 and details in errors. For one hint, omit hints and provide top-level text."),
		obj(map[string]any{
			"hints":        map[string]any{"type": "array", "description": locale.Text(t.language, "Preferred: ordered hints array; each item uses text/asset_ids/traffic_refs. Returned ids match input length/order."), "items": obj(map[string]any{"text": str(locale.Text(t.language, "Hint text")), "asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "traffic_refs": HintTrafficSchema(t.language)})},
			"text":         str(locale.Text(t.language, "Single hint, for example focus on authenticated interfaces")),
			"traffic_refs": HintTrafficSchema(t.language),
			"asset_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Optional anchored asset IDs (zero/one/many)")},
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Hints    []hintItem `json:"hints"`
				hintItem            // Single-item mode uses top-level text/asset_ids.
			}
			_ = json.Unmarshal(in, &a)
			batch := len(a.Hints) > 0
			items := a.Hints
			if !batch {
				items = []hintItem{a.hintItem}
			}

			ids := make([]int64, len(items))
			errs := map[string]string{}
			var addedTexts []string
			for i, it := range items {
				id, err := t.addOneHint(it)
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				ids[i] = id
				addedTexts = append(addedTexts, strings.TrimSpace(it.Text))
			}
			if len(addedTexts) > 0 {
				// Wake once per batch, preferring notifyHint to record one trigger containing
				// all new strategy hints so the planner knows why it woke and sees their content.
				// Without that callback, use a bare notify; hints remain in the graph for reading.
				switch {
				case t.notifyHint != nil:
					t.notifyHint(addedTexts)
				case t.notify != nil:
					t.notify()
				}
			}

			if !batch { // Single-item mode retains the original response.
				if e, bad := errs["0"]; bad {
					return actool.Errorf(e), nil
				}
				return actool.Text(fmt.Sprintf("hint added: %d", ids[0])), nil
			}
			out := map[string]any{"ids": ids}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		})
}

// killWorkTool lets the planner terminate a single running work (by intent id).
func (t *ToolSet) killWorkTool() actool.CoreTool {
	return t.writeExpTool("kill_work", locale.Text(t.language, "Terminate a running intent/worker that has drifted or is no longer useful. It becomes stopped and is not automatically reclaimed. Inspect get_worker_output before deciding."),
		obj(map[string]any{"intent_id": idp(locale.Text(t.language, "Intent ID to terminate (worker handle)"))}, "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.killWork == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "kill_work is currently unavailable")), nil
			}
			var a struct {
				IntentID json.RawMessage `json:"intent_id"`
			}
			_ = json.Unmarshal(in, &a)
			id := pid(a.IntentID)
			if id <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id is required")), nil
			}
			node, err := t.ts.GetNode(id)
			if err != nil || node == nil || node.Kind != db.KindIntent {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id must belong to this task; associated-task intents are read-only")), nil
			}
			if err := t.killWork(id); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return actool.Text(fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Termination signal sent to the worker for intent %d"), id)), nil
		})
}

// steerWorkTool lets the planner inject a mid-run course-correction into a running
// work WITHOUT killing it: the message reaches the worker before its next tool call,
// which re-plans its next step (already-gathered context is kept). For in-intent
// nudges such as stop X/focus Y; an entirely wrong direction needs kill_work and a new intent.
func (t *ToolSet) steerWorkTool() actool.CoreTool {
	return t.writeExpTool("steer_work", locale.Text(t.language, "Steer a running intent/worker without interruption or lost progress; the worker receives the correction before its next action. Use for within-intent guidance such as stop X/focus Y. If the entire direction is wrong, use kill_work then create another intent. Inspect get_worker_output first."),
		obj(map[string]any{
			"intent_id": idp(locale.Text(t.language, "Intent ID to steer (worker handle)")),
			"message":   str(locale.Text(t.language, "Clear correction specifying what the worker should stop and focus on instead")),
		}, "intent_id", "message"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.steerWork == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "steer_work is currently unavailable")), nil
			}
			var a struct {
				IntentID json.RawMessage `json:"intent_id"`
				Message  string          `json:"message"`
			}
			_ = json.Unmarshal(in, &a)
			id := pid(a.IntentID)
			if id <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id is required")), nil
			}
			node, err := t.ts.GetNode(id)
			if err != nil || node == nil || node.Kind != db.KindIntent {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id must belong to this task; associated-task intents are read-only")), nil
			}
			if strings.TrimSpace(a.Message) == "" {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "message is required")), nil
			}
			if err := t.steerWork(id, a.Message); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return actool.Text(fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Correction sent to the worker for intent %d; effective before its next action"), id)), nil
		})
}

// getWorkerOutput returns final output, or output up to interruption, by intent ID.
func (t *ToolSet) getWorkerOutput() actool.CoreTool {
	return t.readExpTool("get_worker_output", locale.Text(t.language, "Get an intent/worker's final output from this task or a directly associated task. Inherited results have source_task_id/inherited=true and are read-only. Normal completion returns its summary; stopped/failed work returns the last output before interruption."),
		obj(map[string]any{"intent_id": idp(locale.Text(t.language, "Intent ID (worker handle)"))}, "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				IntentID json.RawMessage `json:"intent_id"`
			}
			_ = json.Unmarshal(in, &a)
			id := pid(a.IntentID)
			if id <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id is required")), nil
			}
			intentNode, err := t.ts.GetNodeWithSources(id)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if intentNode == nil || intentNode.Kind != db.KindIntent {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id does not belong to this task or a directly associated task")), nil
			}
			acts, _, err := t.ts.ActivityListWithSources(id, 0, 1000)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			var chosen, fallback *db.Activity
			for i := range acts {
				switch acts[i].Kind {
				case "result":
					chosen = &acts[i]
					fallback = &acts[i]
				case "text":
					fallback = &acts[i]
				}
			}
			pick := chosen
			if pick == nil {
				pick = fallback
			}
			if pick == nil {
				if intentNode.Inherited {
					return jsonResult(inheritedMap(map[string]any{
						"intent_id": id, "final_text": locale.Text(locale.FromContext(ctx), "(This worker has no output yet)"),
					}, intentNode.SourceTaskID))
				}
				return actool.Text(locale.Text(locale.FromContext(ctx), "(This worker has no output yet)")), nil
			}
			detail, _ := t.ts.ActivityDetailWithSources(pick.ID)
			if detail == "" {
				detail = pick.Summary
			}
			result := map[string]any{
				"intent_id": id, "final_text": detail,
				"summary": pick.Summary, "is_error": pick.IsError,
			}
			if intentNode.Inherited {
				inheritedMap(result, intentNode.SourceTaskID)
			}
			return jsonResult(result)
		})
}

// traceSteps renders summary-only trace rows, re-truncating each summary to 100
// chars — the stored summary is capped at 200 for the UI transcript; the trace
// tools want it tighter since a whole work's step list is many rows.
func traceSteps(acts []db.Activity) []map[string]any {
	steps := make([]map[string]any, 0, len(acts))
	for i := range acts {
		step := map[string]any{
			"step_id": acts[i].ID, "kind": acts[i].Kind, "tool": acts[i].Tool,
			"is_error": acts[i].IsError, "summary": firstLine(acts[i].Summary, 100),
		}
		if acts[i].Inherited {
			inheritedMap(step, acts[i].SourceTaskID)
		}
		steps = append(steps, step)
	}
	return steps
}

// getWorkerTrace exposes a work's execution PROCESS (not just its final output):
// list step summaries, keyword-search within one work, or pull full detail of a
// few specific steps. Thinking steps are excluded everywhere.
func (t *ToolSet) getWorkerTrace() actool.CoreTool {
	return t.readExpTool("get_worker_trace",
		locale.Text(t.language, "Inspect an intent/worker's execution trace, unlike get_worker_output which returns only the conclusion. Three modes:\n")+
			locale.Text(t.language, "1. intent_id only: per-step summaries up to 100 characters with step_id; action outline, not full output.\n")+
			locale.Text(t.language, "2. intent_id + q: summaries for steps whose summary or full output matches the keyword; use mode 3 for content.\n")+
			locale.Text(t.language, "3. intent_id + step_ids: full detail for at most 5 steps. Extra IDs are reported in notice/omitted_step_ids.\n")+
			locale.Text(t.language, "Locate step_ids using mode 1/2, then retrieve full output with mode 3. Thinking steps are excluded. Directly associated tasks' historical traces are supported with source_task_id/inherited=true and are read-only."),
		obj(map[string]any{
			"intent_id": idp(locale.Text(t.language, "Intent ID (worker handle)")),
			"q":         str(locale.Text(t.language, "Optional keyword matching summary/full output; mutually exclusive with step_ids")),
			"step_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "step_ids from summary/search to retrieve in full, at most 5; extra IDs appear in omitted_step_ids")},
			"limit":     intp(locale.Text(t.language, "Optional result limit for summary/search")),
		}, "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				IntentID json.RawMessage   `json:"intent_id"`
				Q        string            `json:"q"`
				StepIDs  []json.RawMessage `json:"step_ids"`
				Limit    int               `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			id := pid(a.IntentID)
			if id <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id is required")), nil
			}
			intentNode, nodeErr := t.ts.GetNodeWithSources(id)
			if nodeErr != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), nodeErr)), nil
			}
			if intentNode == nil || intentNode.Kind != db.KindIntent {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "intent_id does not belong to this task or a directly associated task")), nil
			}
			// ③ detail drill-down by step ids, thinking excluded by the store.
			if len(a.StepIDs) > 0 {
				// Dedup + drop invalid ids first so garbage/duplicates don't eat into
				// the per-call cap. detail is returned in full (untruncated), so the
				// cap bounds one tool result; over the cap we serve the first N and
				// tell the model exactly which ids were deferred, instead of erroring
				// and forcing it to re-plan the call.
				const maxStepIDs = 5
				var ids []int64
				seen := make(map[int64]bool)
				for _, raw := range a.StepIDs {
					if v := pid(raw); v > 0 && !seen[v] {
						seen[v] = true
						ids = append(ids, v)
					}
				}
				var omitted []int64
				if len(ids) > maxStepIDs {
					omitted = append(omitted, ids[maxStepIDs:]...)
					ids = ids[:maxStepIDs]
				}
				acts, err := t.ts.ActivityByIDsWithSources(ids)
				if err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				steps := make([]map[string]any, 0, len(acts))
				for i := range acts {
					if acts[i].NodeID == nil || *acts[i].NodeID != id || acts[i].Inherited != intentNode.Inherited ||
						(acts[i].Inherited && acts[i].SourceTaskID != intentNode.SourceTaskID) {
						continue
					}
					step := map[string]any{
						"step_id": acts[i].ID, "kind": acts[i].Kind, "tool": acts[i].Tool,
						"is_error": acts[i].IsError, "detail": acts[i].Detail,
					}
					if acts[i].Inherited {
						inheritedMap(step, acts[i].SourceTaskID)
					}
					steps = append(steps, step)
				}
				result := map[string]any{"intent_id": id, "steps": steps, "returned_step_ids": ids}
				if len(omitted) > 0 {
					// returned_step_ids/omitted_step_ids let the model decide programmatically
					// whether another call is worth it; the notice states the same in prose.
					result["omitted_step_ids"] = omitted
					result["notice"] = fmt.Sprintf(
						locale.Text(locale.FromContext(ctx), "At most %d full steps per request. Returned the first %d (%v); omitted %d: %v.")+
							locale.Text(locale.FromContext(ctx), "Do not fetch the remaining steps if this is enough. If more detail is necessary, call again with the omitted step_ids."),
						maxStepIDs, len(ids), ids, len(omitted), omitted)
				}
				if intentNode.Inherited {
					inheritedMap(result, intentNode.SourceTaskID)
				}
				return jsonResult(result)
			}
			// ①/② summary stream, optionally keyword-filtered; 100-char summaries.
			var acts []db.Activity
			var err error
			if strings.TrimSpace(a.Q) != "" {
				acts, err = t.ts.ActivityTraceSearchWithSources(id, a.Q, a.Limit)
			} else {
				acts, err = t.ts.ActivityTraceWithSources(id, a.Limit)
			}
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			result := map[string]any{"intent_id": id, "steps": traceSteps(acts)}
			if intentNode.Inherited {
				inheritedMap(result, intentNode.SourceTaskID)
			}
			return jsonResult(result)
		})
}

// searchAllWorkerTraces keyword-searches EVERY work's process in this task — for
// finding what a worker saw but never wrote back as a fact. Returns only matching
// summaries (≤100 chars), each tagged with its intent_id for follow-up drill-down.
func (t *ToolSet) searchAllWorkerTraces() actool.CoreTool {
	return t.readExpTool("search_all_worker_traces",
		locale.Text(t.language, "Usually unnecessary because most context is already supplied. Search other workers' execution traces in this task by q to recover observations not saved as facts, such as paths/tokens/errors.")+
			locale.Text(t.language, "Your own intent's steps are excluded because they are already in your context.")+
			locale.Text(t.language, "Returns only matching step summaries (up to 100 characters) with intent_id. Use get_worker_trace(intent_id, step_ids=[...]) for full content."),
		obj(map[string]any{
			"q":     str(locale.Text(t.language, "Keyword searched in all workers' step summaries and full output")),
			"limit": intp(locale.Text(t.language, "Optional result limit, default 100")),
		}, "q"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Q     string `json:"q"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.Q) == "" {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "q is required")), nil
			}
			// Exclude the caller's own intent; its trace is already present in worker context.
			acts, err := t.ts.ActivityTraceSearchAllWithSources(t.ownerNode, a.Q, a.Limit)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			hits := make([]map[string]any, 0, len(acts))
			for i := range acts {
				var intent int64
				if acts[i].NodeID != nil {
					intent = *acts[i].NodeID
				}
				hit := map[string]any{
					"intent_id": intent, "step_id": acts[i].ID, "worker": acts[i].Worker,
					"kind": acts[i].Kind, "tool": acts[i].Tool, "is_error": acts[i].IsError,
					"summary": firstLine(acts[i].Summary, 100),
				}
				if acts[i].Inherited {
					inheritedMap(hit, acts[i].SourceTaskID)
				}
				hits = append(hits, hit)
			}
			return jsonResult(map[string]any{"query": a.Q, "hits": hits})
		})
}

// listWorkerTraces gives a worker (which has no graph_overview and can't see the
// intent graph) a lightweight index of the works in this task — intent_id +
// one-line summary + state — so it can DISCOVER which works to inspect via
// get_worker_trace. Without this a worker only knows intent_ids that come back
// from search_all_worker_traces hits. Excludes still-open intents (not yet run →
// no process to inspect).
func (t *ToolSet) listWorkerTraces() actool.CoreTool {
	return t.readExpTool("list_worker_traces",
		locale.Text(t.language, "Usually unnecessary because most information is supplied. List executed worker/intent entries in this task: intent_id, one-sentence direction summary, and state.")+
			locale.Text(t.language, "Workers cannot browse the exploration graph directly. Use this to locate relevant work, then get_worker_trace(intent_id) for steps and get_worker_trace(intent_id, step_ids=[...]) for details.")+
			locale.Text(t.language, "Includes running/done/exhausted/blocked/stopped intents, not unexecuted open ones. Your scope remains your assigned intent; other traces are only to reuse observations and avoid duplicate work."),
		obj(map[string]any{
			"q":     str(locale.Text(t.language, "Optional summary keyword filter")),
			"limit": intp(locale.Text(t.language, "Optional result limit, default 50")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Q     string `json:"q"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			limit := a.Limit
			if limit <= 0 {
				limit = 50
			}
			all, err := t.ts.ListByKindWithSources(db.KindIntent, 500)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			q := strings.ToLower(strings.TrimSpace(a.Q))
			out := make([]map[string]any, 0, limit)
			for _, n := range all {
				if n.Inherited && n.State == "running" {
					continue
				}
				switch n.State {
				case "running", "done", "exhausted", "blocked", "stopped": // has run → has a process
				default:
					continue
				}
				var p map[string]any
				_ = json.Unmarshal(n.Payload, &p)
				summary, _ := p["summary"].(string)
				if q != "" && !strings.Contains(strings.ToLower(summary), q) {
					continue
				}
				item := map[string]any{"intent_id": n.ID, "summary": summary, "state": n.State}
				if n.Inherited {
					inheritedMap(item, n.SourceTaskID)
				}
				out = append(out, item)
				if len(out) >= limit {
					break
				}
			}
			return jsonResult(map[string]any{"works": out})
		})
}

// PlannerTools is the read + intent-generation + goal-judgement tool set.
func (t *ToolSet) PlannerTools() []actool.CoreTool {
	return []actool.CoreTool{
		t.graphOverview(), t.listFindings(), t.listFacts(), t.nodeDetail(),
		// cold-digest §6.1: restore folded cold nodes (digest body → members → detail).
		t.expandDigest(),
		t.getWorkerOutput(), t.getWorkerTrace(), t.searchAllWorkerTraces(), t.listGoals(), t.addIntent(), t.proveGoal(), t.goalMet(),
		t.killWorkTool(), t.steerWorkTool(),
		// report_finding lets planners register vulnerabilities they have actually confirmed.
		t.addFinding(),
		// list_companies exposes company scope/counts and company_id for ownership reasoning.
		t.listCompanies(),
		// list_assets supports DSL searches across available assets by domain, fingerprint,
		// port, or status, complementing list_untested_assets' untested-in-scope view.
		t.listAssets(),
		// add_company_scope adds domains/IP/CIDR/ICP/keywords to company scope and claims matches.
		t.addCompanyScope(),
		// add_task_scope explicitly expands this task's scope, the coverage denominator.
		t.addTaskScope(),
		// list_untested_assets supports type-filtered, paginated inspection to decide on further testing.
		t.listUntestedAssets(),
	}
}
