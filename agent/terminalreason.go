package agent

import (
	"context"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"strings"
	"time"

	"github.com/Autumn-27/norma/harness"
)

// runTrace retains the latest tool call so an interrupted run can identify the
// operation that was still in flight.
type runTrace struct {
	startedAt time.Time
	id        string
	name      string
	input     string
	at        time.Time
	pending   bool
}

func (t *runTrace) start(id, name, input string) {
	t.id, t.name, t.input, t.at, t.pending = id, name, input, time.Now(), true
}

func (t *runTrace) done(id string) {
	if id == t.id {
		t.pending = false
	}
}

var reasonHint = map[harness.TerminalReason]string{
	harness.ReasonCompleted:         "The model ended the round normally without a text summary; use this round's tool records for facts and assets",
	harness.ReasonMaxTurns:          "MaxTurns reached: the SDK performed wrap-up and saved facts/assets. The intent becomes exhausted so the planner can continue with another direction; this is not treated as failure",
	harness.ReasonTimeout:           "MaxDuration reached: running tools are interrupted for immediate wrap-up and identified facts/assets are saved. The intent becomes exhausted",
	harness.ReasonModelError:        "Model/API failure (network, authentication, rate limit, provider 5xx, etc.). After retries the intent becomes blocked; little exploration may have occurred. Inspect get_worker_trace before deciding to retry or change approach",
	harness.ReasonBlockingLimit:     "Context reached its hard limit and the request was blocked before sending; narrow the intent or reduce tool output",
	harness.ReasonPromptTooLong:     "Prompt too long and compaction retries exhausted; execution cannot continue",
	harness.ReasonImageError:        "The model does not support this round's multimodal content; use a vision-capable model or avoid image tool output",
	harness.ReasonStopHookPrevented: "A Stop hook prevented completion and execution could not resume; check whether task Guard rules are too restrictive",
	harness.ReasonHookStopped:       "A tool or hook stopped execution, for example for an out-of-scope target or disabled command; inspect the last tool_result interception explanation",
	harness.ReasonAbortedStreaming:  "Run cancelled while the model was streaming output",
	harness.ReasonAbortedTools:      "Run cancelled during tool execution",
}

// terminalText renders a terminal event with no final text into a compact summary
// and a Markdown detail block.
func terminalText(ctx context.Context, term *harness.Terminal, tr *runTrace) (string, string) {
	reason := term.Reason
	aborted := reason == harness.ReasonAbortedStreaming || reason == harness.ReasonAbortedTools
	// Prompt may return ctx.Err directly without a terminal event. Preserve the
	// cancellation cause instead of falling back to an empty/unknown terminal reason.
	if reason == "" && ctx.Err() != nil {
		aborted = true
	}

	var sum string
	if aborted {
		_, short, _, ok := AbortReason(ctx)
		if !ok {
			short = locale.Text(locale.FromContext(ctx), "Cancellation cause unavailable")
		}
		stage := locale.Text(locale.FromContext(ctx), "execution")
		switch reason {
		case harness.ReasonAbortedStreaming:
			stage = locale.Text(locale.FromContext(ctx), "model output")
		case harness.ReasonAbortedTools:
			stage = locale.Text(locale.FromContext(ctx), "tool execution")
		}
		sum = locale.Text(locale.FromContext(ctx), "(Run interrupted: ") + short + locale.Text(locale.FromContext(ctx), "; stopped during ") + stage + progressSuffix(term, tr, locale.FromContext(ctx)) + locale.Text(locale.FromContext(ctx), "; incomplete)")
	} else if reason == harness.ReasonMaxTurns || reason == harness.ReasonTimeout {
		sum = locale.Text(locale.FromContext(ctx), "(Run budget limit reached (") + string(reason) + locale.Text(locale.FromContext(ctx), "); facts saved during wrap-up") + progressSuffix(term, tr, locale.FromContext(ctx)) + locale.Text(locale.FromContext(ctx), "; no text summary)")
	} else {
		hint := terminalReasonHint(reason, locale.FromContext(ctx))
		sum = locale.Text(locale.FromContext(ctx), "(No text summary; terminal state ") + terminalReasonLabel(reason) + ": " + firstLine(hint, 80) + ")"
	}

	var b strings.Builder
	b.WriteString(sum)
	b.WriteString("\n\n")
	displayReason := terminalReasonLabel(reason)
	fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Terminal state**: `%s` - %s\n"), displayReason, terminalReasonHint(reason, locale.FromContext(ctx)))
	if aborted {
		code, _, why, ok := AbortReason(ctx)
		if ok {
			fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Interruption cause** (`%s`): %s\n"), code, why)
		} else {
			b.WriteString(locale.Text(locale.FromContext(ctx), "- **Interruption cause**: unavailable; the canceller may not have attached a named cause through context.WithCancelCause\n"))
		}
	}
	if term.Err != nil {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Underlying error**: `%v`\n"), term.Err)
	}
	if aborted && strings.TrimSpace(term.Text) != "" {
		b.WriteString(locale.Text(locale.FromContext(ctx), "- **Partial output generated before cancellation**:\n\n"))
		b.WriteString(term.Text)
		b.WriteString("\n\n")
	}
	if term.Turns > 0 {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Executed**: %d model turns\n"), term.Turns)
	}
	if !tr.startedAt.IsZero() {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Run duration**: %s\n"), roundDur(time.Since(tr.startedAt)))
	}
	if u := term.Usage; u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens > 0 {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Total tokens**: input %d / output %d / cache read %d / cache write %d\n"),
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
	if tr.name == "" {
		b.WriteString(locale.Text(locale.FromContext(ctx), "- **Tool calls**: run ended before any tool was called\n"))
	} else if tr.pending {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Tool running at interruption**: `%s` (running for %s, **no result returned**)\n\n  ```json\n  %s\n  ```\n"),
			tr.name, roundDur(time.Since(tr.at)), firstLine(tr.input, 300))
	} else {
		fmt.Fprintf(&b, locale.Text(locale.FromContext(ctx), "- **Last tool before interruption**: `%s` (returned normally)\n"), tr.name)
	}
	return sum, b.String()
}

func terminalReasonLabel(reason harness.TerminalReason) string {
	if reason == "" {
		return "context_canceled"
	}
	return string(reason)
}

func terminalReasonHint(reason harness.TerminalReason, langs ...locale.Lang) string {
	if hint := reasonHint[reason]; hint != "" {
		return locale.Text(locale.First(langs), hint)
	}
	if reason == "" {
		return locale.Text(locale.First(langs), "Run context was cancelled without an underlying Terminal event")
	}
	return locale.Text(locale.First(langs), "Unknown terminal state; harness may have added a TerminalReason. Update reasonHint")
}

func progressSuffix(term *harness.Terminal, tr *runTrace, langs ...locale.Lang) string {
	var parts []string
	if term.Turns > 0 {
		parts = append(parts, fmt.Sprintf(locale.Text(locale.First(langs), "%d turns"), term.Turns))
	}
	if !tr.startedAt.IsZero() {
		parts = append(parts, roundDur(time.Since(tr.startedAt)))
	}
	if len(parts) == 0 {
		return ""
	}
	return locale.Text(locale.First(langs), ", elapsed ") + strings.Join(parts, " / ")
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	case d < time.Hour:
		return d.Round(time.Second).String()
	default:
		return d.Round(time.Minute).String()
	}
}
