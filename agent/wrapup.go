package agent

import (
	"github.com/sebastian93921/artifex/locale"
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up prompts are injected by SDK settlement when an agent exhausts MaxTurns
// or reaches run_seconds/MaxDuration. They ask the agent to persist identified
// results before giving a final sentence, preventing unsaved work at termination.
//
// Each agent may override its body in agents.wrapup_prompt; empty uses defaults.
// Tool restrictions remain code-owned; turn budgets use their separate configured policy.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in defaults are keyed by agent. Workers reuse settleWrapUpPrompt from
// worker.go; planner/mainagent have distinct prompts and custom agents use the generic one.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults gives each agent its own settlement turn budget, overridable with a positive value.
// Defaults are ten turns to allow persistence; unlisted agents use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "This planning round is nearly out of steps. Only this round is ending; graph changes will wake you again, and the task is not terminating. Save conclusions reached this round without inventing intents merely to wrap up; zero intents remains valid. (1) Submit already justified directions that should be dispatched now in one add_intent batch. (2) Mark goals proven by facts/findings met using prove_goal. (3) Record dependent multi-step chains in TodoWrite for the next wake. Then end this round; no summary text is needed."

const mainAgentWrapUpDefault = "Your step budget is nearly exhausted and this interaction is ending. Start no new exploration or operations. Give the user ONE standalone plain-text sentence summarizing progress, key conclusions, and the recommended next step."

const genericWrapUpDefault = "Your budget is about to expire. Save completed but unsaved results, then output ONE standalone plain-text sentence summarizing your actions and key conclusions; it will be shown as this run's result."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string, langs ...locale.Lang) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        BuiltinPromptText(resolveWrapup(agentKey), locale.First(langs)),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// Task-level timeout settlement, distinct from per-run budget settlement.
//
// Per-run settlement means this run exhausted its budget; task timeout means the
// entire engagement is ending. Planner instructions differ: preserve future planning
// for per-run settlement, but stop dispatching and judge goals at task timeout.

// WrapupTaskTimeoutOverride and its turn override are database-backed settings
// for worker/planner agents.task_timeout_wrapup_prompt and task_timeout_wrapup_max_turns.
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "The ENTIRE TASK has reached its deadline and is ending; this is not just your run budget. This is the last opportunity: (1) Save ALL identified but unsaved results: insert_assets for new assets, record_fact for conclusions/facts, report_finding for confirmed vulnerabilities. (2) Start NO new commands or probes. (3) Finally output ONE standalone plain-text sentence summarizing the key conclusions for this intent."

const plannerTaskTimeoutDefault = "The ENTIRE TASK has reached its deadline and is ending, not just this round. Make a final objective assessment from ALL current facts and findings. Use prove_goal to mark met only goals proven by evidence; do not overlook proven goals. Generate NO new intents: they will not be executed. End after the assessment; no summary text is needed."

// TaskTimeoutWrapupDefault supplies UI placeholders and reset-to-default values.
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // Unconfigured agents, such as mainagent/chat, return empty.
}

// Nonempty DB override wins over the built-in default. Empty means this agent
// has no task-timeout prompt, so callers should use per-run settlement.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // Default to the per-run turn budget.
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true: Timeout means the task deadline, using task-timeout settlement;
//     MaxTurns means turns ran out while task time remains, using per-run settlement.
//   - clamped=false: both reasons use per-run settlement, equivalent to wrapupSettlement.
//
// harness chooses PromptByReason from the actual termination reason, not at build time.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool, langs ...locale.Lang) *harness.Settlement {
	perRun := BuiltinPromptText(resolveWrapup(agentKey), locale.First(langs))
	st := &harness.Settlement{
		Prompt:        perRun, // Fallback, also used for both reasons without clamping.
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := BuiltinPromptText(resolveTaskTimeoutWrapup(agentKey), locale.First(langs)); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // Task deadline reached.
				harness.ReasonMaxTurns: perRun, // Turn budget exhausted while task time remains.
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
