package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "User paused the task",
		"The user paused the task through POST /api/tasks/{id}/control with action=pause. This Planner/Worker run was cancelled; running intents return to frontier(open), and will be claimed and restarted from the beginning on resume.")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator agent paused the task",
		"The orchestrator called pause_task. This Planner/Worker run was cancelled; running intents return to frontier(open) and execute again after resume.")
	AbortTaskDeleted = cause("task_deleted", "Task was deleted",
		"The task is being deleted through DELETE /api/tasks/{id}. The deletion barrier cancelled its active Planner, Worker, and main agent; this run's result will no longer be used.")
	AbortPausedOnReload = cause("paused_on_reload", "Backend restored the task's paused state",
		"At startup, the backend restored the persisted paused state from the database. This run was cancelled; normally no agent is running during restoration.")
	AbortGoalMet = cause("goal_met", "Planner marked task complete",
		"The planner marked the task done after confirming its objectives, then cancelled active workers. Their intents are marked stopped, not failed.")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task wrap-up drain expired",
		"After task timeout, workers were allowed to wrap up, but the 90-second drain grace expired. Hard cancellation marks intents exhausted; facts/assets already saved during wrap-up remain.")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner terminated this intent",
		"The planner used kill_work to terminate this intent, typically because it drifted or no longer justified further work. It is marked stopped and is not automatically reclaimed.")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this worker intent",
		"The user paused the running worker. This call was cancelled and the intent became paused. Registered intents, facts, findings, and activities remain; resuming restarts execution from the beginning.")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this worker intent",
		"The user deleted the running worker. This call was cancelled. After the worker leaves the write section, soft deletion marks the intent deleted and retains outputs; hard deletion removes it and downstream nodes supported only by it, according to the user's selected mode.")
	AbortWorkFinished = cause("work_finished", "Worker finished normally",
		"The worker finished normally and detachWork released its context resources. This is not an interruption; seeing it in an interruption message indicates a race between cancellation and completion events.")
	AbortPausedRaceGuard = cause("paused_race_guard", "New run blocked while paused",
		"The engine refused a new execution context while the task was paused, preventing a claim/pause race from starting a worker. Claimed intents return to frontier.")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this conversation turn",
		"The user clicked Stop to cancel this main-agent or conversation-agent turn. Existing activity records remain; another message can be sent.")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause stopped main-agent chat",
		"Pausing the task also cancelled its active main-agent conversation. Existing activity records remain; resuming the task does not replay this message automatically.")
	AbortChatTurnFinished = cause("chat_turn_finished", "Conversation finished normally",
		"The conversation turn finished normally and the server released its context resources. This is not an interruption; seeing it in an interruption message indicates a cancellation/completion race.")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend process is shutting down",
		"The backend received SIGINT or SIGTERM and is restarting, updating, or shutting down. Active agents are cancelled; leftover running intents reset to open and execute again after restart.")
	AbortRunHardTimeout = cause("run_hard_timeout", "Per-run hard timeout backstop triggered",
		"This run exceeded its soft wall-clock budget and grace period. A model request or tool likely failed to return, preventing normal turn-boundary wrap-up. Inspect the last unfinished tool call before interruption.")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, locale.Text(locale.FromContext(ctx), ac.Short), locale.Text(locale.FromContext(ctx), ac.Text), true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", locale.Text(locale.FromContext(ctx), "Parent context reached its deadline"),
			locale.Text(locale.FromContext(ctx), "Parent context reached its deadline without a named WithTimeoutCause reason: ") + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", locale.Text(locale.FromContext(ctx), "Canceller did not attach a named cause"),
			locale.Text(locale.FromContext(ctx), "Parent context was cancelled without a named context.WithCancelCause reason. Register the cause in agent/cancelcause.go and attach it at the cancellation site."), true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
