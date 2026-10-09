package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"strings"
	"sync"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(locale.Text(locale.ServerDefault(), "\n\n[Your planning to-do list, retained from the previous wake]:\n"))
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString(locale.Text(locale.ServerDefault(), "Proceed only with steps whose prerequisites are complete or whose required facts exist. Update TodoWrite, marking fact-supported steps completed. Do not duplicate pending/in_progress steps."))
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//
// "finding": a worker reported a finding for IntentID; Detail is its summary.
// "goal": the human added one or more goals through the main agent's set_goals.
// Goals contains this call's new goal texts; set_goals supports batching.
// "goal_deleted": the human removed a goal in overview management; Detail is the removed text.
// "goal_edited": the human edited a goal in overview management; OldGoal -> NewGoal.
// "cancelled": the human deleted IntentID; Detail is the deletion reason. The intent is
//
//	stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // For cancelled: summary captured before deletion, since a hard-deleted node cannot be queried.
	Goals    []string // For goal: one or more new goal texts from this set_goals call.
	OldGoal  string   // For goal_edited: previous goal text.
	NewGoal  string   // For goal_edited: updated goal text.
	Hints    []string // For hint: one or more new hint texts from this add_hint call.
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(locale.Text(locale.ServerDefault(), "\n\n[Actual changes triggering this round; read before choosing new directions]:"))
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator (main agent) added an objective: %s. Add a direction for this unmet goal if not already covered."), ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator (main agent) added %d objectives: %s. Add directions for each unmet goal not already covered."), len(ev.Goals), strings.Join(ev.Goals, "；")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator (main agent) added a strategic hint: %s. It is saved in the exploration graph; adjust/add directions if not already covered."), ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator (main agent) added %d strategic hints: %s. They are saved in the exploration graph; adjust/add directions accordingly."), len(ev.Hints), strings.Join(ev.Hints, "；")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator deleted objective: %s. Reassess remaining goals/directions; no longer dispatch intents for this goal."), ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The operator changed the objective from \"%s\" to \"%s\". Adjust directions and stop dispatching obsolete ones."), ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The worker for intent #%d (%s) reported a finding: %s"), ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer the summary captured at deletion; intentSummary cannot read a hard-deleted node.
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The user deleted intent #%d: %s. Reason: %s. It will no longer execute; replan accordingly."), ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n- The worker for intent #%d (%s) finished with conclusion: %s"), ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf(locale.Text(locale.ServerDefault(), "; new fact IDs from this intent: %s "), fids))
			}
		}
	}
	b.WriteString(locale.Text(locale.ServerDefault(), "\n(For full details use node_detail / get_worker_output / list_findings.)"))
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12、#15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, "、")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return locale.Text(locale.ServerDefault(), "(Failed to retrieve output)")
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return locale.Text(locale.ServerDefault(), "(This worker has no output yet)")
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + locale.Text(locale.ServerDefault(), " … (truncated; use get_worker_output for full content)")
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return locale.Text(locale.ServerDefault(), "\n\n[Current situation: prefetched graph_overview, identical to the tool return; fetch node_detail/list_facts only as needed]:\n") + string(b)
}

// plannerDefaultTmpl is the built-in editable body (section [A]) of the planner prompt,
// seeded into agent_prompts. Goal uses {{.Goal}}; intermediate-artifact output rules
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = "You are the planner in an authorized penetration-testing system, awakened whenever the graph changes. Read the situation, assess objectives, and add exploration intents ONLY for genuinely new, uncovered directions. You plan; you do not execute. This round may produce clarified/new intents or objective judgments, never perform the work itself.\n\nTask objective: {{.Goal}}\n\nHow many intents should this round produce?\n- Highest-priority minimum: if objectives remain unmet AND there are no open or running intents (frontier_open=0 and running_intents empty), produce at least one intent advancing the objective. With no running work or queued direction, zero intents stalls the task. Even if known directions appear only in recent_done, start or continue one using the done/exhausted/blocked rules below.\n- Otherwise, zero intents is normal only with a reason: all directions are already covered by open/running intents, or the next step depends on an unfinished worker's output. Never duplicate an existing intent with different wording. Wait for graph updates when prerequisites are not yet available.\n- Dispatch genuinely uncovered directions without unresolved dependencies, or untested in-scope surfaces while objectives remain unmet. Zero is not a default excuse to stop.\n\nOn each wake:\n1. The full graph_overview result is attached below; do not call it again. It includes the original task title/objective/root, asset counts, goals/statuses, open/running/recent_done intents, sites_without_endpoints, facts (distinct from vulnerabilities), and recent_facts {id,summary,confidence?}.\n- Exploration nodes (goals/intents/facts/findings) are task-local. The asset graph is globally shared; counts represent global in-scope assets, not assets exclusive to this task. Ignore irrelevant assets.\n- Lineage: intents include parents (upstream facts/intents) and yields (resulting facts/findings); recent_facts include from_intent. Use this to understand which direction produced a fact and whether facts support a new direction.\n- Negative/uncertain observations (closed port, not injectable) are observations, not final truth. Read node_detail(id) evidence before trusting them. Strong evidence, confidence=observed, and exhausted methods can temporarily close a direction. Missing evidence, a single superficial probe, or confidence=inferred means unresolved. If in scope and not covered elsewhere, normally dispatch verification to confirm or refute it. Recheck the SAME negative direction at most ONCE; respect a second negative result with reasonable evidence.\n- Fetch detail only when needed: list_facts (newest first, default 20, q/before filters, total/has_more), list_findings, node_detail(id), list_assets (q, type/company_id/task_id, pagination, id/ids), asset_neighbors. Do not load the entire shared asset graph by default.\n2. Assess objectives: use prove_goal(goal_id, evidence_id, reason) to mark an unmet goal met only when a finding/fact proves it. Marking the final unmet goal automatically completes the task; there is no separate complete-all action.\n- Quantitative acceptance is mandatory. Before prove_goal, compare measured graph_overview values (coverage.pct, findings_total, required flags/privileges) with the actual requirement. Never mark met early because the core result seems sufficient. Required coverage 100% with measured 40% is unmet: dispatch work to close the gap.\n3. Optional tiny initial reconnaissance: only at startup, with almost no facts and insufficient information to describe the first intent, use a very small number of read-only probes (such as 1-2 curl requests for the homepage/fingerprint). The ONLY valid output is a more precise intent, never vulnerability discovery/verification/exploitation or endpoint/directory/parameter enumeration.\n- Once worker facts exist (facts>0 or recent_facts nonempty), do not probe yourself. Use existing facts to dispatch or end the round; deeper investigation belongs to workers.\n- Even at startup, perform at most THREE probes. Stop immediately if you are investigating deeply rather than choosing a direction: endpoint/directory enumeration, trying IDs, decoding chains, repeated endpoint probes, injection/access-control/vulnerability tests are worker work. Dispatch them as intents.\n- If existing facts suffice, do not probe at all.\n4. Choose new directions. Restraint means avoiding duplicates, not minimizing useful work. While goals remain unmet, ask what deeper, uncovered in-scope approach advances them. Intents are open-ended exploration directions, not a fixed menu. Compare each with open, running, and recent_done:\n- Covered by open/running: do not generate it.\n- In recent_done: inspect its state first.\n  done: do not repeat it unchanged. Whether the route is exhausted depends on yielded facts, not state alone. Reopen only with a materially new mechanism (fact, asset, parameter, distinctly different approach), explaining the difference in summary. Rewording or hoping a retry works is insufficient.\n  exhausted (budget ended mid-exploration, partial write-back) / blocked (model/network failure, little progress): inspect get_worker_trace/get_worker_output. Resume genuinely near-breakthrough work from its saved progress; retry a route that never ran because of an external failure; change approach if repeatedly stuck at the same point. Decide from actual traces, not state alone.\n- Entirely uncovered new direction: generate it.\n- All known directions covered by open/running: end the round and wait. If only recent_done remains and goals are unmet, the minimum rule above requires starting or continuing a direction.\n- Prefer depth on high-value paths (RCE, privilege escalation, exfiltration) over shallow checks solely to equalize coverage. Coverage is an acceptance floor, not the objective itself.\n- Preserve diverse mechanisms. If current work clusters on one route while a fundamentally different in-scope surface/asset/chain remains uncovered, add that direction rather than synonyms on the same line. Prefer 2-3 distinct routes until actual evidence justifies concentrating effort. Diversity NEVER overrides operating constraints: excluded surfaces, ports, hosts, or operations must never become intents.\nSequential exploitation chains must be dispatched step by step, not in parallel. Record the full dependent chain in TodoWrite, one step each; dispatch only steps with actual prerequisites available. After their facts arrive, dispatch the next step and mark satisfied steps completed. Do not split one action into duplicate intents (identifying a trigger and triggering it). Parallel intents are only for genuinely independent dimensions.\n5. Submit selected directions in ONE add_intent call (intents array, at most FOUR highest-value intents, not separate calls).\n- summary: one natural-language sentence with the complete target address, action, and reason. This is the main deduplication signal.\n- asset_ids: IDs of actual target assets from list_assets (zero/one/many). Include them whenever targeting a site, endpoint, parameter, or host, and include all for multi-asset work. Leave empty only for global reconnaissance without a concrete asset.\n- parent_ids: upstream facts/intents/findings supporting the direction; include all combined facts. Leave empty for a genuinely new top-level direction.\nDo not duplicate or pad work. Dispatch useful deeper, uncovered directions while objectives remain unmet. Be concise, focused, and efficient."

func plannerSystem(goal, dataDir, workDir string, langs ...locale.Lang) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()}, langs...)
	return body + artifactSpec(workDir, langs...)
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner", locale.FromContext(ctx))
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools plus Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash defaults.
	// When coverage is disabled, omit add_task_scope/list_untested_assets from the prompt.
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Put changing situation data (completed intents and prefetched graph) in this
	// round's user input, leaving static system instructions cacheable across rounds.
	// It may be compacted during long rounds, but planner rounds are usually short.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// The context carries task deadline/final-round state (taskclock.go). In the final
	// round, append task-timeout settlement to user input as the round's operating
	// instruction: assess goals one last time without generating intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += locale.Text(locale.FromContext(ctx), "\n\n[Final task wrap-up: this round's special instruction overrides normal planning above]:") + resolveTaskTimeoutWrapup("planner")
	}
	// Create this task's working directory: <workDir>/tasks/<taskID>.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir, locale.FromContext(ctx))
	if p.wantConstraints() {
		sysBody += constraintBlock(ts, locale.FromContext(ctx)) // Inject operating constraints to bound exploration.
	}
	system, boundary := deferredSystem(sysBody, def)
	// Planner has no separate wall-clock budget. Clamp MaxDuration to the task's
	// remaining time: timeout uses task settlement, MaxTurns uses per-run settlement.
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil, locale.FromContext(ctx))
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped, locale.FromContext(ctx))
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    append(system, outputLanguageInstruction(locale.FromContext(ctx))),
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Route through capture proxy and trust its CA for re-signed HTTPS certificates.
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Optional web search: ddgs needs no key, brave-free needs BraveKey, tavily needs TavilyKey.
		// WebSearchProxy is independent of capture MITM; empty means direct.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subprocesses inherit the proxy and trusted CA.
		WorkingDir:            taskDir,                              // Task working directory: <workDir>/tasks/<taskID>.
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 means unlimited; with a deadline, use remaining task time.
		Compaction:            compactionConfig(p.compactionWindow()),
		// Share planning todos across wakes: sessions are new, but the store persists sequential chains.
		Todos: p.todoFor(ts.ID()),
		// At this round's step budget, SDK settlement saves decisions: add_intent,
		// prove_goal, and TodoWrite chains. It does not end planning; future wakes continue.
		// Deadline-clamped runs use PromptByReason; see wrapupSettlementForTask.
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // Non-streaming profiles use Provider.Complete.
		MaxTokens:    p.maxTokens(),    // 0 omits the limit and uses the server default.
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental noa compaction stores persistent archives under <workDir>/noa/<SessionID>.
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// Situation data (completed intents and full graph) goes in this round's user input,
	// alongside instructions and persistent todos, which are regenerable model notes.
	// With concrete changes, point to the changes block. Without them (heartbeat,
	// hint, or resume), do not falsely claim graph changes; ask to review running work.
	lead := locale.Text(locale.FromContext(ctx), "Specific changes just occurred (see Actual changes below). Plan the next step from them:")
	if len(triggers) == 0 {
		lead = locale.Text(locale.FromContext(ctx), "This is a scheduled heartbeat wake with no specific change signal; the graph may be unchanged. Review running intents: use steer_work for stalled/drifting work and kill_work for an entirely wrong direction. Then assess goals and whether new directions are needed:")
		// A heartbeat with no open/running intents means exploration has stalled: no
		// workers or queued directions. Require new directions instead of an empty review round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = locale.Text(locale.FromContext(ctx), "This scheduled heartbeat finds NO open/running intents: no workers or queued directions, so exploration has stalled. First assess achievement from the situation below. If goals remain unmet, you MUST produce one or more advancing intents that do not duplicate existing intents; zero is not allowed:")
		}
	}
	input := lead + situational + locale.Text(locale.FromContext(ctx), "\n\nAssess goals from the situation above. Use prove_goal individually only for truly achieved outcomes/confirmed target vulnerabilities. Mandatory minimum: if goals remain unmet and frontier_open=0 with running_intents empty, produce at least one advancing intent; no work is running or queued, so zero stalls the task. New intents may be omitted only while open/running work advances the goal or the goals are met.") +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration interrupts running tools and performs settlement on a live context;
	// no external hard context is needed here. ctx carries pause/kill/shutdown only.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
