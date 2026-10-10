package llmpool

import (
	"context"
	"iter"
	"log"
	"sync"
	"time"

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
	// maxTimeout caps a single admitted call; see SetMaxTimeout.
	maxTimeout time.Duration

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

// SetMaxTimeout caps every call admitted through this limiter at d (whole
// lifecycle: queue wait for non-hold callers, work for hold callers whose
// AcquireHold already waited outside any budget). 0 = no per-call deadline.
// Set once right after construction, before the limiter is published.
func (l *Limiter) SetMaxTimeout(d time.Duration) { l.maxTimeout = d }

// MaxTimeout reports the per-call cap; used by the compactor to size its own
// budget from the profile instead of a hardcoded default.
func (l *Limiter) MaxTimeout() time.Duration { return l.maxTimeout }

// Name returns the profile name this limiter was built for (for logs).
func (l *Limiter) Name() string { return l.name }

// acquire takes a slot, waiting for one to free up. Returns false when ctx is
// done first (caller stopped, task stopped, timeout) — no slot is taken.
func (l *Limiter) acquire(ctx context.Context) bool {
	if l.holdActive(ctx) {
		return true // the context already holds a reserved slot on this limiter
	}
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

// holdCtxKey carries an active slot hold through the call context.
type holdCtxKey struct{}

type holdInfo struct{ lim *Limiter }

// AcquireHold reserves a slot for the lifetime of the returned release func.
// The returned context carries the hold: provider calls made under it skip this
// limiter's acquire (the hold IS the reserved slot), so per-call queueing never
// happens — e.g. a worker run leasing a slot for a whole intent. waited reports
// how long the reservation itself blocked, so callers can keep that time OUT of
// their own wall-clock budgets. Returns an error when ctx ends first.
func (l *Limiter) AcquireHold(ctx context.Context) (context.Context, func(), time.Duration, error) {
	start := time.Now()
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx, func() {}, 0, ctx.Err()
	}
	waited := time.Since(start)
	cctx := context.WithValue(ctx, holdCtxKey{}, holdInfo{lim: l})
	var mu sync.Mutex
	released := false
	return cctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if released {
			return
		}
		released = true
		<-l.slots
	}, waited, nil
}

// holdActive reports whether ctx carries an active hold on THIS limiter.
func (l *Limiter) holdActive(ctx context.Context) bool {
	h, ok := ctx.Value(holdCtxKey{}).(holdInfo)
	return ok && h.lim == l
}

// Stream implements llm.Provider with the concurrency cap applied.
func (l *Limiter) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	if l.maxTimeout > 0 {
		tctx, cancel := context.WithTimeout(ctx, l.maxTimeout)
		inner := l.stream(tctx, req)
		return func(yield func(llm.StreamEvent, error) bool) {
			defer cancel() // the deadline lives exactly as long as the iteration
			inner(yield)
		}
	}
	return l.stream(ctx, req)
}

func (l *Limiter) stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
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
	if l.maxTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.maxTimeout)
		defer cancel()
	}
	if !l.acquire(ctx) {
		return llm.Message{}, "", llm.Usage{}, context.Cause(ctx)
	}
	defer l.release()
	return l.next.Complete(ctx, req)
}
