package llmpool

import (
	"context"
	"iter"
	"log"
	"sync"

	"github.com/sebastian93921/artifex/locale"
	"github.com/Autumn-27/norma/llm"
)

// Limiter caps how many requests run in flight against one provider at the same
// time. Self-hosted runtimes (vLLM, Ollama, a shared DGX box) serve a fixed
// number of concurrent sessions; exceeding the budget occupies every slot and
// disrupts other users of the endpoint. A Limiter queues excess callers until a
// slot frees up, so the engine degrades to waiting instead of monopolizing.
//
// The zero-cap form is the identity: NewLimiter with max <= 0 returns next
// unchanged, so "unlimited" stays the default and existing profiles behave
// exactly as before.
//
// The slot is held for the whole request — for Stream, until the caller stops
// consuming or the stream ends — because the in-flight HTTP request occupies
// the endpoint for that entire duration. Limiter is safe for concurrent use;
// wrap one provider per endpoint (the per-profile provider cache already
// guarantees a single instance per profile).
type Limiter struct {
	next  llm.Provider
	name  string        // profile name for logs
	slots chan struct{} // capacity = max concurrent requests

	logMu    sync.Mutex
	loggedWa bool // log the first wait per Limiter, not one line per request
}

// NewLimiter caps next at max concurrent in-flight requests. max <= 0 (or a nil
// next) returns next as-is.
func NewLimiter(next llm.Provider, name string, max int) llm.Provider {
	if next == nil || max <= 0 {
		return next
	}
	return &Limiter{next: next, name: name, slots: make(chan struct{}, max)}
}

// Max returns the configured concurrency cap (for status endpoints).
func (l *Limiter) Max() int { return cap(l.slots) }

// acquire takes a slot, waiting for one to free up. Returns false when ctx is
// done first (caller stopped, task stopped, timeout) — no slot is taken.
func (l *Limiter) acquire(ctx context.Context) bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
	}
	// Slot not immediately available: this call is now queued behind the cap.
	// Log once per Limiter so long queues are visible without spamming.
	l.logMu.Lock()
	if !l.loggedWa {
		l.loggedWa = true
		log.Printf(locale.Text(locale.FromContext(ctx), "[llmpool] All %d request slots for LLM profile %q are busy; further requests queue until one frees up"), cap(l.slots), l.name)
	}
	l.logMu.Unlock()
	select {
	case l.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (l *Limiter) release() { <-l.slots }

// Stream implements llm.Provider with the concurrency cap applied.
func (l *Limiter) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if !l.acquire(ctx) {
			yield(llm.StreamEvent{}, context.Cause(ctx))
			return
		}
		defer l.release()
		for ev, err := range l.next.Stream(ctx, req) {
			if !yield(ev, err) {
				return // caller stopped consuming; release the slot
			}
		}
	}
}

// Complete implements llm.Provider with the concurrency cap applied.
func (l *Limiter) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if !l.acquire(ctx) {
		return llm.Message{}, "", llm.Usage{}, context.Cause(ctx)
	}
	defer l.release()
	return l.next.Complete(ctx, req)
}
