package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Worker is an LLM work agent (docs §4.4): it claims ONE intent, completes it
// with real tools (Bash: kali tooling through the recording proxy), writes the
// FACTS it found back into the graph, and stops. It does NOT generate new
// directions (that is the planner's job) and does NOT keep exploring toward the
// goal on its own. Multiple workers run concurrently as goroutines.
// WebSearchOpts is the web-search backend selection the server pushes into each
// agent (planner/worker/main). Enabled=false leaves the web_search tool off.
// Backend is "ddgs" (no key), "brave-free" (BraveKey required), "tavily"
// (TavilyKey required), or "deepseek" (DeepSeek* required, filled from the
// active LLM profile). It maps directly onto agentcore.Options.
// Proxy is a dedicated egress proxy for the search request (http/https/socks5),
// independent of the traffic-recording MITM proxy — set it when the search endpoint
// is only reachable via a VPN/SOCKS proxy. Empty = direct.
//
// DeepSeek differs from other backends: it has no standalone search API. Search
// is available only through its Anthropic-compatible messages endpoint's server
// web_search_20250305 tool, consuming a model call and making requests on DeepSeek's
// servers, outside the local proxy and traffic capture.
type WebSearchOpts struct {
	Enabled   bool
	Backend   string
	BraveKey  string
	TavilyKey string
	Proxy     string
	// DeepSeek credentials come from the active LLM profile's official Anthropic-format
	// endpoint, not a separate setting, and change with the active profile.
	DeepSeekBaseURL string
	DeepSeekAPIKey  string
	DeepSeekModel   string
}

type Worker struct {
	findingRecorder FindingRecorder
	prov            llm.Provider
	model           string
	workDir         string
	proxyAddr       string
	proxyCACert     string            // recording proxy's CA cert path (for WebFetch HTTPS verify)
	webSearch       WebSearchOpts     // web_search tool backend selection (off by default)
	tx              *transcript.Store // raw LLM conversation persistence (nil = off)
	window          int               // context window in tokens (for compaction)
	windowFn        func() int        // optional dynamic task-chain minimum
	maxTurns        int               // max agent turns per run (0 = unlimited)
	// runTimeout is the wall-clock budget for the main exploration of one intent
	// (0 = unlimited). When it fires, the run is cut and a settlement round is
	// forced so already-identified facts get written back instead of being lost.
	runTimeout time.Duration
	// extraTools are host-provided tools (e.g. traffic query, oast) appended to
	// the worker's graph write-back tools.
	extraTools []actool.CoreTool
	// injectConstraints resolves whether this task's operation constraints get
	// injected into the worker system prompt. Read per run so the settings toggle
	// takes effect without rebuilding the agent. nil = inject (default).
	injectConstraints func() bool
	// nonStreamingFn resolves whether this run uses the non-streaming (Complete)
	// path. Read per run so a profile/task toggle takes effect without rebuilding
	// the agent. nil = streaming (default).
	nonStreamingFn func() bool
	// noaEnabledFn resolves whether this run uses the experimental noa context-
	// compression mechanism. Read per run, like nonStreaming. nil = off (built-in
	// compaction).
	noaEnabledFn func() bool
	// maxTokensFn resolves the per-reply output cap in tokens, on the same
	// per-run basis. nil or 0 = send no cap and let the endpoint decide.
	maxTokensFn func() int
}

// WorkerSessionID returns the stable transcript key used by a worker intent.
// Worker slots are reusable, so the intent id (rather than work#N) is the
// session identity. Keep this helper public so the Worker message API and UI
// can refer to exactly the conversation that will be resumed.
func WorkerSessionID(explorationID, intentID int64) string {
	return fmt.Sprintf("exp%d-worker-i%d", explorationID, intentID)
}

const workerChatMarkerPrefix = "<!-- ARTIFEX_WORKER_CHAT:"

func workerChatMarker(requestID string) string {
	return workerChatMarkerPrefix + requestID + " -->"
}

func hasWorkerChatMessage(messages []llm.Message, requestID string) bool {
	marker := workerChatMarker(requestID)
	for _, message := range messages {
		if message.Role == llm.RoleUser && strings.Contains(message.Text(), marker) {
			return true
		}
	}
	return false
}

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default). Read per
// run so a profile or task-chain toggle takes effect without rebuilding.
func (w *Worker) SetNonStreaming(fn func() bool) { w.nonStreamingFn = fn }

func (w *Worker) nonStreaming() bool { return w.nonStreamingFn != nil && w.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (w *Worker) SetNoaEnabled(fn func() bool) { w.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (w *Worker) SetMaxTokens(fn func() int) { w.maxTokensFn = fn }

func (w *Worker) maxTokens() int {
	if w.maxTokensFn == nil {
		return 0
	}
	return w.maxTokensFn()
}

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the worker system prompt. nil = inject (default).
func (w *Worker) SetConstraintInject(fn func() bool) { w.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (w *Worker) wantConstraints() bool { return w.injectConstraints == nil || w.injectConstraints() }

// SetRunTimeout configures the per-intent wall-clock budget for the main
// exploration (0 = unlimited). When it fires, the SDK settlement phase still runs
// so facts are never lost to a timeout. Safe to call before Execute.
func (w *Worker) SetRunTimeout(run time.Duration) {
	w.runTimeout = run
}

// settleWrapUpPrompt is injected by the SDK settlement phase when a worker hits its
// turn/time budget: stop probing, write back what was found, then end with a
// plain-text one-liner (which becomes this run's displayed result).
const settleWrapUpPrompt = "Your budget is about to expire. Do not run any more commands or probes. First save every identified but unsaved result: new assets with insert_assets, conclusions/facts with record_fact, and confirmed vulnerabilities with report_finding. Finally, output ONE standalone plain-text sentence summarizing what you did and the key conclusions; it will be shown as this run's result."

func NewWorker(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int, extra ...actool.CoreTool) *Worker {
	return &Worker{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, extraTools: extra}
}

// defaultToolsExcept returns actool.DefaultTools() minus the named tools (by
// CoreTool.Name()). Used to trim SDK default tools an agent shouldn't have.
func defaultToolsExcept(exclude ...string) []actool.CoreTool {
	drop := make(map[string]bool, len(exclude))
	for _, n := range exclude {
		drop[n] = true
	}
	all := actool.DefaultTools()
	out := make([]actool.CoreTool, 0, len(all))
	for _, t := range all {
		if !drop[t.Name()] {
			out = append(out, t)
		}
	}
	return out
}

func (w *Worker) SetCompactionWindowResolver(fn func() int) { w.windowFn = fn }

func (w *Worker) compactionWindow() int {
	if w.windowFn != nil {
		return w.windowFn()
	}
	return w.window
}

// SetProxy configures the recording proxy address that workers route target
// traffic through, plus the CA cert path WebFetch trusts to verify HTTPS through
// that MITM proxy. Empty addr disables the hint.
func (w *Worker) SetProxy(addr, caCert string) { w.proxyAddr, w.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for this worker (off by default).
func (w *Worker) SetWebSearch(o WebSearchOpts) { w.webSearch = o }

// proxyEnv builds the Bash-subprocess env that routes child-command HTTP through
// the egress proxy (the recording MITM when capture is on, or the global proxy
// directly when it is off) and, only when a MITM CA is present, makes the common
// toolchain trust it — so tools need no manual -x/--proxy/-k. Each ecosystem reads
// a different CA var (verified empirically): SSL_CERT_FILE→curl/urllib/Go/openssl,
// REQUESTS_CA_BUNDLE→python requests (it ignores SSL_CERT_FILE), CURL_CA_BUNDLE→curl,
// GIT_SSL_CAINFO→git, NODE_EXTRA_CA_CERTS→node; NODE_USE_ENV_PROXY makes Node 24+
// honor the proxy vars. ALL_PROXY is set too so a socks5 egress proxy (which curl
// only reads from ALL_PROXY, not HTTP(S)_PROXY) works in the capture-off path.
// Empty proxyAddr → nil (direct, unchanged env).
func proxyEnv(proxyAddr, caCert string) []string {
	if proxyAddr == "" {
		return nil
	}
	env := []string{
		"HTTP_PROXY=" + proxyAddr, "HTTPS_PROXY=" + proxyAddr,
		"http_proxy=" + proxyAddr, "https_proxy=" + proxyAddr,
		"ALL_PROXY=" + proxyAddr, "all_proxy=" + proxyAddr, // socks5 egress: curl reads only this
		"NODE_USE_ENV_PROXY=1", // Node 24+: honor HTTP(S)_PROXY in built-in fetch/http
	}
	if caCert != "" {
		env = append(env,
			"SSL_CERT_FILE="+caCert,
			"CURL_CA_BUNDLE="+caCert,
			"REQUESTS_CA_BUNDLE="+caCert,
			"GIT_SSL_CAINFO="+caCert,
			"NODE_EXTRA_CA_CERTS="+caCert,
		)
	}
	return env
}

// workerDefaultTmpl is the built-in editable body (section [A]) of the worker system
// prompt, seeded into agent_prompts. Traffic guidance and intermediate-artifact rules
// are NOT here — they are code-owned and appended by workerSystem after rendering
// are code-owned sections [B]/[C], so editing the stored body cannot remove them.
const workerDefaultTmpl = "You are the executor (work agent) in an authorized penetration-testing system. You receive ONE intent: a one-sentence direction for exploration. Complete that intent, write the findings to the knowledge graph, then stop and return.\n\nMandatory boundaries:\n1. Work only on your assigned intent. If you notice a promising lead outside it (an error-disclosed path, a link to another asset, or a different exploitation chain), mention it briefly in the fact summary for the planner.\n2. An initial obstacle (filtered payload, 404, or an injection with no output) does not establish that a direction is exhausted. Complete the reasonable methods within this intent before concluding.\n3. Operate only within authorization. Any Operating constraints at the top of the system prompt are the highest-priority boundaries: check every command/probe against them before execution and do not violate them, even if requested by the intent.\n\nWrite results back immediately as you discover them. Results count only once saved in the graph; do not lose them by waiting until the step budget expires. Use the correct graph:\n- New assets/resources -> insert_assets (asset graph): subdomains, services, endpoints, fingerprints, credentials, and other assets themselves. Do not put conclusions here; use record_fact.\n- Conclusions/facts -> record_fact (exploration graph, supply intent_id). Consolidate observations into ONE fact: a one-sentence summary and expanded detail grounded in actual execution. Do not create one fact per property. Default to one fact per intent, combining related details. Use the facts array only for genuinely independent conclusions that cannot be combined. Write only new information; do not paraphrase existing facts or re-register confirmations with no new information. Record only what you actually observed. Include concise evidence (command plus the one or two most probative output lines; details belong in detail), and confidence=observed for direct observations or inferred for deductions.\n- Confirmed vulnerabilities -> report_finding (exploration graph, include PoC and intent_id). Use this ONLY when you actually triggered the vulnerability in this run and obtained reproducible evidence (request/response or command output). Version/fingerprint-to-CVE matches, suspicious parameters, vulnerability databases, changelogs, or code diffs are not confirmation. Never substitute CVE lookup or patch comparison for triggering the issue. If suspicious but not triggered, use record_fact with confidence=inferred, explain the suspicion and failure to trigger, and leave it for the planner.\n\nAfter finishing the intent, summarize in one sentence what you did and which facts you saved."

// workerTrafficBlock is section [B], injected only when capture and its tools exist.
// traffic capture (recording) is on — i.e. the traffic_* tools actually exist.
// Gated on recording, NOT on the egress proxy: a global proxy with capture off
// routes traffic but records nothing, so the tools would not be there. Not stored,
// not editable.
func workerTrafficBlock(recording bool, langs ...locale.Lang) string {
	if !recording {
		return ""
	}
	return locale.Text(locale.First(langs), "\n\n**Traffic tools**:\n- traffic_search / traffic_get / traffic_blob: review responses and previously visited resources. Check recorded traffic first; do not curl the same URL again. traffic_search REQUIRES host and defaults to three lightweight index records (id/method/url/status/resp_len, no response content); explicitly increase limit if needed. body_contains searches request/response bodies (at least 3 characters, supports substrings and Unicode) for passwords, keys, errors, internal addresses, etc. Use traffic_get(id) for original content; oversized bodies appear as @blob sha256:<hash>, retrieved in sections using traffic_blob(hash).")
}

// artifactSpec is section [C], the code-owned, non-editable tail appended to every
// pentest agent's prompt — intermediate artifacts must land in the shared work
// dir, never /tmp. Guaranteed present regardless of how the DB body is edited.
func artifactSpec(dir string, langs ...locale.Lang) string {
	return locale.Text(locale.First(langs), "\n\n**Intermediate artifact rules**: Write ALL scripts, payloads, captured response bodies, temporary data, and other intermediate artifacts to this task's working directory **") + dir + locale.Text(locale.First(langs), "** (relative paths resolve here; this absolute path is also allowed). **Do not write to /tmp or other absolute paths.**")
}

// workerArtifactSpec is the worker's section [C]; the engine pre-creates its per-intent directory.
// by the engine (ensureRunDir), so it just writes relative paths there — no manual
// mkdir, no cross-worker name collisions.
func workerArtifactSpec(runDir string, langs ...locale.Lang) string {
	return locale.Text(locale.First(langs), "\n\n**Intermediate artifact rules**: Write ALL scripts, payloads, captured response bodies, temporary data, and other intermediate artifacts to this intent's dedicated working directory **") + runDir + locale.Text(locale.First(langs), "** (already created; use relative paths, no manual mkdir needed). **Do not write to /tmp or other absolute paths.**")
}

// ensureRunDir builds and creates an agent's working directory under base:
// <base>/tasks/<taskID> for planner/main; <base>/tasks/<taskID>/i<intentID> for a
// worker (intentID<=0 → task dir only). The "tasks/" segment groups per-task dirs
// symmetrically with the chat agent's "sessions/<sessionID>". Best-effort mkdir — on
// failure, writes fail the same way an unwritable CWD would.
func ensureRunDir(base string, taskID, intentID int64) string {
	dir := filepath.Join(base, "tasks", strconv.FormatInt(taskID, 10))
	if intentID > 0 {
		dir = filepath.Join(dir, "i"+strconv.FormatInt(intentID, 10))
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// cmdOutDir is the SDK large-tool-output spill dir under an agent's run dir.
func cmdOutDir(dir string) string { return filepath.Join(dir, "cmd-output") }

func workerSystem(proxyAddr, caCert, dataDir, runDir string, langs ...locale.Lang) string {
	body := renderSystem("worker", workerDefaultTmpl, WorkerVars{ProxyAddr: proxyAddr, DataDir: dataDir, Now: nowStr()}, langs...)
	// caCert is present only when the recording MITM is on, which is exactly when
	// the traffic_* tools are registered — so it gates the traffic-tool note.
	// Optional finding guidance is added for every role after tool resolution.
	return body + workerTrafficBlock(caCert != "", langs...) + workerArtifactSpec(runDir, langs...)
}

// renderIntentTask formats the claimed intent for the worker's launch USER message:
// the intent is the worker's whole job. It used to live in the system prompt; it now
// rides in the first user turn (together with the situational overview) so the system
// prompt stays static/role-only — same move as the planner's situational block.
// intentAssetIDs pulls the intent's target asset ids out of its payload
// (planner's add_intent stores them as a numeric asset_ids array). nil on absence
// or malformed payload.
func intentAssetIDs(intent *db.Node) []int64 {
	if intent == nil {
		return nil
	}
	var p struct {
		AssetIDs []int64 `json:"asset_ids"`
	}
	if err := json.Unmarshal(intent.Payload, &p); err != nil {
		return nil
	}
	return p.AssetIDs
}

func renderIntentTask(intent *db.Node) string {
	return fmt.Sprintf(locale.Text(locale.ServerDefault(), "\n\n[Your assigned intent: the only task for this run; execute it, record facts, and stop]:\n%s\nIntent id: %d (supply it to record_fact / report_finding)"), string(intent.Payload), intent.ID)
}

// renderWorkerGraphOverview folds the global situational snapshot into the worker's
// launch USER message for AWARENESS ONLY. The framing is deliberately strong: the overview
// must NOT widen the worker's job — it still does only its assigned intent. Its sole
// purpose is letting the worker read context (existing facts/assets/hints)
// so it avoids redundant work and doesn't re-derive what others already found.
func renderWorkerGraphOverview(data map[string]any) string {
	// Coverage guides the planner's breadth decisions, but conflicts with the worker's
	// single-intent mandate. Remove it from the worker view. This map belongs to this
	// worker invocation, so deleting the key does not affect the planner.
	delete(data, "coverage")
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back silently: the worker just won't have the global context
	}
	return locale.Text(locale.ServerDefault(), "\n\n[Global exploration situation: read-only context for your intent]:\n") +
		locale.Text(locale.ServerDefault(), "The current task overview below helps avoid duplicating others' findings and understand your intent's relationship to the wider task.\n") +
		locale.Text(locale.ServerDefault(), "Think broadly while exploring this intent, but do not execute other intents; the planner assigns those to other workers. Save every valuable lead (cross-asset relationships, another chain's entry, wider suspicious behavior) as a fact for the planner. This is an essential result, not optional; let the planner assess it rather than withholding it.\n") +
		string(b)
}

// Execute runs one intent. hooks (the per-task Guard) gates every tool call; may
// be nil. emit, if non-nil, receives one ActivityRecord per execution step.
// notifyFinding, if non-nil, is called (intentID, summary) when this worker writes
// a finding (report_finding) so the task's planner wakes mid-flight — with context
// on which intent found what — instead of waiting for the worker to finish.
// Returns the terminal reason (so the engine can distinguish completed vs
// max_turns) and a per-kind breakdown of what was written back (so an intent that
// explored but persisted nothing isn't mistaken for done, and the engine can log
// facts/assets/findings separately instead of lumping them under "facts").
func (w *Worker) Execute(ctx context.Context, name string, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, intent *db.Node, hooks harness.HookRunner, emit func(db.Activity), enr EnrichTrigger, notifyFinding func(int64, string)) (harness.TerminalReason, WriteCounts, error) {
	return w.execute(ctx, name, taskID, as, ts, intent, hooks, emit, enr, notifyFinding, "", "")
}

// ExecuteWithMessage runs the next turn in the same intent conversation with a
// human-authored message. The HTTP handler does not edit the transcript;
// agentcore records the message as a normal user turn when this Worker starts.
// This keeps Worker continuation identical to the regular agent chat flow.
func (w *Worker) ExecuteWithMessage(ctx context.Context, name string, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, intent *db.Node, hooks harness.HookRunner, emit func(db.Activity), enr EnrichTrigger, notifyFinding func(int64, string), requestID, message string) (harness.TerminalReason, WriteCounts, error) {
	return w.execute(ctx, name, taskID, as, ts, intent, hooks, emit, enr, notifyFinding, strings.TrimSpace(requestID), strings.TrimSpace(message))
}

func (w *Worker) execute(ctx context.Context, name string, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, intent *db.Node, hooks harness.HookRunner, emit func(db.Activity), enr EnrichTrigger, notifyFinding func(int64, string), requestID, message string) (harness.TerminalReason, WriteCounts, error) {
	tsx := NewToolSet(ts, name, locale.FromContext(ctx))
	tsx.SetFindingRecorder(w.findingRecorder)
	tsx.SetTaskID(taskID)
	coverageEnabled := as == nil || as.CoverageEnabled(taskID)
	tsx.SetCoverageEnabled(coverageEnabled)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetOwnerNode(intent.ID)         // assets this worker discovers anchor to its intent → visible to the task
	tsx.SetEnrich(enr)                  // async DNS/HTTP auto-completion for assets this worker writes
	tsx.SetNotifyFinding(notifyFinding) // report_finding immediately wakes the planner with the producing intent and finding.
	// base = built-in worker tools ∪ host tools (traffic) ∪ default tools (incl. Bash);
	// then augment with the agent's visible skills/MCP. During the SDK settlement
	// phase, Bash is hidden via Settlement.DisabledTools (no local gating needed).
	base := append(tsx.WorkerTools(), w.extraTools...)
	// Omit MultiEdit/Glob/Grep for workers: Edit handles precise changes and Bash handles
	// searches. Keep Read/Write/Edit/LS/Bash/Sleep, reducing low-value tool selection.
	base = append(base, defaultToolsExcept("MultiEdit", "Glob", "Grep")...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts), IntentID: intent.ID})
	tools, def, cleanup := AugmentTools(ctx, "worker", base)
	defer cleanup()

	// The intent is the worker's sole mandate throughout a run. Put it, startup rules,
	// and anchored target assets in the system prompt, rebuilt each run and never compacted.
	// This keeps the mandate available in long/resumed runs even if the first transcript
	// message disappears. Sacrificing cross-intent prompt-cache reuse is intentional.
	// Unlike planners, workers have a single mandate. Only the global overview stays
	// in the startup user message; it can safely become stale or be compacted away.
	// Engine-created per-intent directory: <workDir>/tasks/<taskID>/i<intentID>.
	runDir := ensureRunDir(w.workDir, taskID, intent.ID)
	// The run-wide intent is not the current tool action. Do not forward it or
	// inherit a parent run's background into the action reviewer.
	ctx = intercept.WithReviewContext(ctx, runDir, intercept.ReviewBackground{})
	overview := renderWorkerGraphOverview(tsx.graphOverviewData())
	sysBody := workerSystem(w.proxyAddr, w.proxyCACert, w.workDir, runDir, locale.FromContext(ctx))
	if w.wantConstraints() {
		sysBody += constraintBlock(ts, locale.FromContext(ctx)) // Inject operating constraints for strict worker compliance.
	}
	// Append intent, anchored assets, and startup instruction after the system body, like constraints.
	sysBody += renderIntentTask(intent)
	if as != nil {
		if ids := intentAssetIDs(intent); len(ids) > 0 {
			if assets, err := as.GetByIDs(ids); err == nil && len(assets) > 0 {
				if b, err := json.Marshal(assets); err == nil {
					sysBody += locale.Text(locale.FromContext(ctx), "\n\nTarget assets referenced by this intent's asset_ids:\n") + string(b)
				}
				// Add explicitly targeted assets to task scope using insertAssets' conservative
				// granularity. ON CONFLICT DO NOTHING plus uq_task_scope prevents duplicates;
				// reruns and retries remain idempotent.
				// Do not accumulate metric scope when coverage is disabled.
				if coverageEnabled {
					for _, a := range assets {
						_ = as.AddAutoScope(taskID, a.Type, a.Domain, a.URL, a.IP)
					}
				}
			}
		}
	}
	sysBody += locale.Text(locale.FromContext(ctx), "\n\nExecute the intent above only. Produce facts, assets, and findings, then stop.")
	system, boundary := deferredSystem(sysBody, def)
	// Context carries the task deadline, clamping this run's budget and selecting settlement text.
	tc := taskClockFrom(ctx)
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, w.runTimeout)
	settle := wrapupSettlement("worker", []string{"Bash"})
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("worker", []string{"Bash"}, clamped)
	}
	opts := agentcore.Options{
		Provider:        w.prov,
		SystemPrompt:    append(system, outputLanguageInstruction(locale.FromContext(ctx))),
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		// WebFetch uses the capture proxy just like curl. Load its CA so MITM re-signed
		// HTTPS certificates validate normally, rather than disabling verification; empty proxy means direct.
		EnableWebFetch: true,
		WebFetchProxy:  w.proxyAddr,
		WebFetchCACert: w.proxyCACert,
		// Optional web search: ddgs needs no key, brave-free needs BraveKey, tavily needs TavilyKey.
		// WebSearchProxy is independent of the capture MITM proxy; empty means direct.
		EnableWebSearch:       w.webSearch.Enabled,
		WebSearchBackend:      w.webSearch.Backend,
		BraveSearchAPIKey:     w.webSearch.BraveKey,
		TavilySearchAPIKey:    w.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: w.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  w.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   w.webSearch.DeepSeekModel,
		WebSearchProxy:        w.webSearch.Proxy,
		// Bash HTTP inherits the capture proxy and CA, requiring neither -x nor -k.
		BashEnv:    proxyEnv(w.proxyAddr, w.proxyCACert),
		WorkingDir: runDir,
		MaxTurns:   w.maxTurns, // 0 = unlimited (configurable in agent management)
		// Wall-clock budget; zero is unlimited. A task deadline clamps it to the smaller
		// of this run's own budget and task time remaining so deadline settlement can run.
		MaxDuration: maxDur,
		// At turn/time budget, SDK settlement hides Bash and saves identified results before stopping.
		// With a task clamp, PromptByReason uses task settlement on timeout and per-run
		// settlement when turns run out first. Unclamped runs use per-run settlement only.
		Settlement: settle,
		// large tool output spills to cmd-output/ with a head + pointer (SDK tool.Capture);
		// Full output is preserved on disk; truncation uses the SDK's default 30000 characters.
		ToolOutputDir: cmdOutDir(runDir),
		Compaction:    compactionConfig(w.compactionWindow()), // long tool-heavy runs stay within the window
		Todos:         actool.NewTodoStore(),                  // Session-local planning todos, discarded at exit.
		NonStreaming:  w.nonStreaming(),                       // Non-streaming profiles use Provider.Complete.
		MaxTokens:     w.maxTokens(),                          // 0 omits the limit and uses the server default.
	}
	if hooks != nil { // typed-nil guard: only set when concrete (avoids harness panic)
		opts.Hooks = hooks
	}
	if w.tx != nil { // persist raw LLM conversation; one file per worked intent
		opts.Transcript = w.tx
		opts.SessionID = WorkerSessionID(ts.ID(), intent.ID)
	}
	intentID := intent.ID
	emitWrap := func(r db.Activity) {
		if emit != nil {
			r.NodeID, r.Worker = &intentID, name
			emit(r)
		}
	}
	// The intent, startup rules, and anchored assets are already in the system prompt.
	// Startup user input carries only the degradable global overview, safe to compact.
	// If overview marshaling unexpectedly fails, use a startup sentence instead of an empty user message.
	input := overview
	if strings.TrimSpace(input) == "" {
		input = locale.Text(locale.FromContext(ctx), "Execute only the intent assigned in the system prompt. Produce facts, assets, and findings, then stop.")
	}

	// Experimental noa compaction stores persistent archives under <workDir>/noa/<SessionID>.
	noaSession := WorkerSessionID(ts.ID(), intent.ID)
	enableNoa(&opts, w.noaEnabledFn, w.workDir, noaSession, noaWarn(noaSession))
	ctx = attachSideCapture(ctx, &opts)
	s := agentcore.NewSession(opts)
	defer s.Close() // release the session's background-task manager (temp dir + processes)

	// Resume prior conversation if this intent was paused/blocked/exhausted and is
	// being re-run. The transcript ID is deterministic per intent, so if a prior
	// session exists the worker continues from where it left off instead of
	// restarting from scratch.
	alreadyRecorded := false
	if w.tx != nil {
		_ = s.Resume(opts.SessionID)
		alreadyRecorded = requestID != "" && hasWorkerChatMessage(s.Messages(), requestID)
		if len(s.Messages()) > 0 && message == "" {
			seedUnlockFromHistory(s.Messages(), def.UnlockSkill)
			input = locale.Text(locale.FromContext(ctx), "Continue execution.")
		} else if len(s.Messages()) > 0 {
			seedUnlockFromHistory(s.Messages(), def.UnlockSkill)
		}
	}
	if message != "" {
		if alreadyRecorded {
			input = locale.Text(locale.FromContext(ctx), "Continue the new intent from the previous human message. Do not repeat completed actions.")
		} else if len(s.Messages()) > 0 {
			input = workerChatMarker(requestID) + locale.Text(locale.FromContext(ctx), "\n[New intent from the human conversation]\n") + message +
				locale.Text(locale.FromContext(ctx), "\n\nAct on this human input immediately; after completion, use context to decide whether the original task needs to continue.")
		} else {
			input += "\n\n" + workerChatMarker(requestID) + locale.Text(locale.FromContext(ctx), "\n[New intent from the human conversation]\n") + message +
				locale.Text(locale.FromContext(ctx), "\n\nPrioritize this human input.")
		}
	}

	// Budgets + settlement are owned by the SDK (MaxTurns/MaxDuration + Settlement):
	// on hit it runs a wrap-up turn and finishes with ReasonMaxTurns/ReasonTimeout.
	// MaxDuration now interrupts an in-flight tool at the wall-clock deadline and
	// enters the wrap-up phase on the live ctx, so a run whose tool overran the budget
	// still settles (no external hard-timeout backstop needed). ctx itself carries only
	// pause / planner kill / shutdown, which the engine distinguishes and re-queues/stops.
	_, reason, err := captureRunSession(ctx, s, input, emitWrap)
	return reason, tsx.Writes(), err
}
