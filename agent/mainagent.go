package agent

import (
	"context"
	"fmt"
	"github.com/sebastian93921/artifex/locale"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// MainAgent is the thin human-interface orchestrator (docs §4.2 / §7). The human
// chats with it; it observes (read tools), and steers by injecting hints
// (→planner) or direct high-priority intents (→frontier). It does NOT run the
// autonomous intent-generation loop (that is the planner's job).
type MainAgent struct {
	findingRecorder FindingRecorder
	prov            llm.Provider
	model           string
	tx              *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window          int                                    // context window in tokens (for compaction)
	windowFn        func() int                             // optional dynamic task-chain minimum
	maxTurns        int                                    // max agent turns per run (0 = unlimited)
	proxyAddr       string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert     string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch       WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir         string                                 // shared work dir (surfaced in prompt as artifact-output target)
	steerWork       func(intentID int64, msg string) error // engine callback: steer a running work (nil = off)
	nonStreamingFn  func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn    func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn     func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
}

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (m *MainAgent) SetNoaEnabled(fn func() bool) { m.noaEnabledFn = fn }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (m *MainAgent) SetNonStreaming(fn func() bool) { m.nonStreamingFn = fn }

func (m *MainAgent) nonStreaming() bool { return m.nonStreamingFn != nil && m.nonStreamingFn() }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (m *MainAgent) SetMaxTokens(fn func() int) { m.maxTokensFn = fn }

func (m *MainAgent) maxTokens() int {
	if m.maxTokensFn == nil {
		return 0
	}
	return m.maxTokensFn()
}

func NewMainAgent(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *MainAgent {
	return &MainAgent{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns}
}

func (m *MainAgent) SetCompactionWindowResolver(fn func() int) { m.windowFn = fn }

func (m *MainAgent) compactionWindow() int {
	if m.windowFn != nil {
		return m.windowFn()
	}
	return m.window
}

// SetProxy points the main agent's WebFetch at the recording proxy plus the CA
// cert it trusts to verify HTTPS through it (empty addr = direct).
func (m *MainAgent) SetProxy(addr, caCert string) { m.proxyAddr, m.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the main agent (off by default).
func (m *MainAgent) SetWebSearch(o WebSearchOpts) { m.webSearch = o }

// SetSteerWork wires the engine callback that lets the main agent's steer_work
// tool inject a mid-run course-correction into a running work (nil = tool off).
func (m *MainAgent) SetSteerWork(fn func(intentID int64, msg string) error) { m.steerWork = fn }

// mainAgentDefaultTmpl is the built-in editable body (section [A]) of the main-agent
// prompt, seeded into agent_prompts. Goal uses {{.Goal}}; the intermediate-artifact
// output rules are code-owned and appended by artifactSpec after rendering.
const mainAgentDefaultTmpl = "You are the main agent of an authorized penetration-testing system and the human operator's interface. Do not explore personally or continuously generate autonomous intents; planning belongs to the planner.\n\n1. Observe: answer progress questions using graph_overview, list_findings, list_facts, list_assets, and get_worker_output.\n2. Translate the operator's direction into platform actions:\n- Change direction, emphasize a vulnerability class, or focus on an area: use add_hint for the planner's next round.\n- Test a specific target immediately: inject a high-priority intent (priority 8-10) with add_intent. A completed task automatically resumes for that intent and returns to completed when it finishes.\n  If all goals are met in graph_overview, first decide whether the request implies a NEW final outcome. If so, restate your inferred goal and ask whether to register it formally. If the user agrees, use set_goals, allowing normal autonomous planning to continue. If they decline or want only a one-off check, use add_intent alone; the task completes again after that worker finishes. No need to ask for an obviously one-off check with no new objective.\n- Steer a running worker (stop approach X, focus on Y): inspect get_worker_output first, then use steer_work. It applies before the next action without interruption or loss of progress. For an entirely different direction, create a new intent with add_intent.\n- Add a final objective: use set_goals. This automatically resumes completed/paused tasks; the planner subsequently evaluates completion. No manual resume is needed.\n- Add/change testing constraints (only this port, no brute force, passive reconnaissance only): use set_constraints with type=allow or type=deny. Constraints are injected into planner/worker prompts on the next round; operators can also edit them in constraint management.\n3. Reply concisely in plain language and state what you did.\n\nCurrent task objective: {{.Goal}}\nNever invent findings. Answer only from actual tool results."

func mainAgentSystem(goal, dataDir, workDir string, langs ...locale.Lang) string {
	body := renderSystem("mainagent", mainAgentDefaultTmpl, MainVars{Goal: goal, DataDir: dataDir, Now: nowStr()}, langs...)
	return body + artifactSpec(workDir, langs...)
}

// Chat handles one human message and returns the assistant reply. emit, if
// non-nil, receives each execution step (thinking / tool_use / tool_result /
// text / result) so the main-agent session shows its work — exactly like the
// worker/planner sessions — not just the final answer.
func (m *MainAgent) Chat(ctx context.Context, taskID int64, mainSeg int, as *db.AssetStore, ts *db.ExplorationStore, goal, message string, emit func(db.Activity), notify, resume func(), notifyGoal, notifyHint func([]string)) (string, error) {
	tsx := NewToolSet(ts, "human", locale.FromContext(ctx))
	tsx.SetFindingRecorder(m.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.SetNotify(notify)         // Debounced general wake for writes without a dedicated callback.
	tsx.SetResumeTask(resume)     // set_goals revives completed/paused tasks to running.
	tsx.SetNotifyGoal(notifyGoal) // set_goals records a planner trigger for newly added human objectives.
	tsx.SetNotifyHint(notifyHint) // add_hint records one planner trigger for the batch of new strategic hints.
	tsx.steerWork = m.steerWork   // enable steer_work tool (nil = unavailable)
	// Domain tools plus Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash defaults.
	// When coverage is disabled, omit add_task_scope/list_untested_assets from the prompt.
	base := append(tsx.DropCoverageTools(tsx.MainAgentTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "mainagent", base)
	defer cleanup()
	// Create this task's working directory: <workDir>/tasks/<taskID>.
	mainDir := ensureRunDir(m.workDir, taskID, 0)
	ctx = intercept.WithReviewWorkingDirectory(ctx, mainDir)
	system, boundary := deferredSystem(mainAgentSystem(goal, m.workDir, mainDir, locale.FromContext(ctx)), def)
	opts := agentcore.Options{
		Provider:        m.prov,
		SystemPrompt:    append(system, outputLanguageInstruction(locale.FromContext(ctx))),
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Route through capture proxy and trust its CA for re-signed HTTPS certificates.
		WebFetchProxy:   m.proxyAddr,
		WebFetchCACert:  m.proxyCACert,
		// Optional web search: ddgs needs no key, brave-free needs BraveKey, tavily needs TavilyKey.
		// WebSearchProxy is independent of the capture MITM proxy; empty means direct.
		EnableWebSearch:       m.webSearch.Enabled,
		WebSearchBackend:      m.webSearch.Backend,
		BraveSearchAPIKey:     m.webSearch.BraveKey,
		TavilySearchAPIKey:    m.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: m.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  m.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   m.webSearch.DeepSeekModel,
		WebSearchProxy:        m.webSearch.Proxy,
		BashEnv:               proxyEnv(m.proxyAddr, m.proxyCACert), // Bash subprocesses inherit the proxy and trusted CA.
		WorkingDir:            mainDir,                              // Task working directory: <workDir>/tasks/<taskID>.
		ToolOutputDir:         cmdOutDir(mainDir),
		MaxTurns:              m.maxTurns,                             // 0 = unlimited (configurable in agent management)
		Compaction:            compactionConfig(m.compactionWindow()), // long chats stay within the window
		Todos:                 actool.NewTodoStore(),                  // Session-local planning todos; discarded at exit.
		// At the step budget, SDK settlement summarizes progress. Prompt/turn count are editable; default ten.
		Settlement:   wrapupSettlement("mainagent", nil, locale.FromContext(ctx)),
		NonStreaming: m.nonStreaming(), // Non-streaming profiles use Provider.Complete.
		MaxTokens:    m.maxTokens(),    // 0 omits the limit and uses the server default.
	}
	if m.tx != nil { // persist raw human↔AI conversation; one accumulating file per segment
		opts.Transcript = m.tx
		// Segment 0 keeps the legacy "exp%d-main" name so existing transcripts still
		// load; each new session (seg>=1) gets its own file for a clean context.
		opts.SessionID = fmt.Sprintf("exp%d-main", ts.ID())
		if mainSeg > 0 {
			opts.SessionID = fmt.Sprintf("exp%d-main-s%d", ts.ID(), mainSeg)
		}
	}
	// Experimental noa compaction stores persistent archives under <workDir>/noa/<SessionID>.
	// Segment-aware session IDs match transcripts so archives and restoration align.
	noaSession := fmt.Sprintf("exp%d-main", ts.ID())
	if mainSeg > 0 {
		noaSession = fmt.Sprintf("exp%d-main-s%d", ts.ID(), mainSeg)
	}
	enableNoa(&opts, m.noaEnabledFn, m.workDir, noaSession, noaWarn(noaSession))
	ctx = attachSideCapture(ctx, &opts)
	s := agentcore.NewSession(opts)
	defer s.Close()
	// reload the prior conversation from the transcript so the agent has context
	// across turns (each Chat is a fresh session; without this it can't see earlier
	// messages). First turn: no file yet → Resume loads nothing and proceeds.
	if m.tx != nil {
		_ = s.Resume(opts.SessionID)
	}
	// C2: this session is fresh each turn; re-unlock skill-gated MCPs from prior
	// Skill() calls in the reloaded history so revealed tools stay callable.
	seedUnlockFromHistory(s.Messages(), def.UnlockSkill)
	text, _, err := captureRunSession(ctx, s, message, func(r db.Activity) {
		if emit != nil {
			r.Worker = "mainagent"
			emit(r)
		}
	})
	return text, err
}
