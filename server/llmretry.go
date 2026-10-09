package server

import (
	"time"

	"github.com/sebastian93921/artifex/agent"
	"github.com/sebastian93921/artifex/db"
)

// Server-side retry policy resolution; see the LLM retry design. Of the five layers:
//   - Connection, empty-response, and same-provider safety-window retries follow the endpoint. Each LLM profile can override
//     global defaults; omitted profile values inherit global settings, and omitted global settings use built-in defaults.
//   - Circuit breaking and intent reruns are process-wide, with global configuration only.
//
// The global policy is one settings row, read only on low-frequency paths (provider construction, worker settlement,
// and saving settings), so another cache is unnecessary. Circuit-breaker parameters are read on every failure;
// applyRetryPolicy therefore pushes those into Registry.

// retryPolicy reads the global policy; a nil DB yields the zero policy (all
// layers on their built-in defaults).
func (s *Server) retryPolicy() db.LLMRetryPolicy {
	if s.m == nil || s.m.pg == nil {
		return db.LLMRetryPolicy{}
	}
	return s.m.pg.LLMRetryPolicy()
}

// resolveRetry layers one profile's override on top of the global policy and
// converts the result into the form agent.Config carries. Rules combine field by
// field, so a profile that only pins an interval still inherits the global count.
func resolveRetry(o db.RetryOverride, pol db.LLMRetryPolicy) agent.RetryConfig {
	connect := o.Connect.Or(pol.Connect)
	empty := o.Empty.Or(pol.Empty)
	stream := o.Stream.Or(pol.Stream)
	return agent.RetryConfig{
		// Preserve attempt semantics here: 0 means default, negative means disabled. SDK MaxRetries and
		// EmptyResponseRetries use the same semantics and resolve them themselves.
		ConnectAttempts: connect.Attempts, ConnectInterval: connect.Interval(),
		EmptyAttempts: empty.Attempts, EmptyInterval: empty.Interval(),
		StreamAttempts: stream.Attempts, StreamInterval: stream.Interval(),
	}
}

// applyProfileRetry fills cfg.Retry for a profile read from the DB.
func (s *Server) applyProfileRetry(cfg *agent.Config, p *db.LLMProfile) {
	if p == nil {
		return
	}
	cfg.Retry = resolveRetry(p.Retry, s.retryPolicy())
}

// Circuit-breaker cooldown defaults match llmpool defaults; override only explicitly configured values.
// Intent rerun defaults are modelErrorRetries/modelErrorRetryBackoff in engine.go.

// applyRetryPolicy pushes the process-wide layers of the policy into the objects
// that consume them on a hot path: the circuit-breaker registry. Called at
// startup and whenever the policy is saved.
func (s *Server) applyRetryPolicy() {
	pol := s.retryPolicy()
	if s.llmHealth != nil {
		s.llmHealth.SetPolicy(pol.Breaker.Attempts, pol.Breaker.Interval())
	}
}

// modelErrorRetryPolicy resolves the intent-level replay knobs (layer ⑤): how
// many times a model_error work is re-run and how long to back off between runs.
func (e *Engine) modelErrorRetryPolicy() (retries int, backoff time.Duration) {
	retries, backoff = modelErrorRetries, modelErrorRetryBackoff
	if e == nil || e.m == nil || e.m.pg == nil {
		return retries, backoff
	}
	rule := e.m.pg.LLMRetryPolicy().Intent
	if rule.Attempts != 0 {
		retries = max(rule.Attempts, 0)
	}
	if d := rule.Interval(); d > 0 {
		backoff = d
	}
	return retries, backoff
}

// emptyTurnNudgeLimit resolves how many empty-turn continuations one work may
// inject (see steerHooks.Stop). It deliberately reuses layer 2's empty-response retry count:
// the two mechanisms address the same user intent. The SDK retries responses without any content blocks
// by resending the original request; this layer handles thinking-only responses with no text or tools by appending
// an instruction to continue from existing reasoning. Resending unchanged context would not resolve that idle turn.
// Detection differs because SDK events include thinking deltas; users asking for empty-response retries
// mean retrying when the model produced no substantive output. Sharing one count across both layers
// matches that expectation.
//
// Read the global policy, not a profile override: failover can change profiles during one run,
// but this is an intent-wide total limit that must not change with endpoints. Semantics match SDK emptyRetries():
// 0 uses defaultEmptyTurnNudges; negative disables idle-turn continuation; positive sets an explicit count.
func (e *Engine) emptyTurnNudgeLimit() int {
	if e == nil || e.m == nil || e.m.pg == nil {
		return defaultEmptyTurnNudges
	}
	switch n := e.m.pg.LLMRetryPolicy().Empty.Attempts; {
	case n == 0:
		return defaultEmptyTurnNudges
	case n < 0:
		return 0
	default:
		return n
	}
}
