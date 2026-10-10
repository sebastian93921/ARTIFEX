package llmpool

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

// A hold reserves a slot for the whole run; per-call acquires under the hold
// context skip the limiter entirely (zero wait), and release frees the slot.
func TestHoldSkipsPerCallAcquire(t *testing.T) {
	p := NewLimiter(stubProvider{}, "test", 1)
	lim, ok := p.(*Limiter)
	if !ok {
		t.Fatal("expected a *Limiter")
	}
	ctx := context.Background()
	hctx, release, waited, err := lim.AcquireHold(ctx)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer release()
	if waited > 50*time.Millisecond {
		t.Fatalf("first hold waited %v on an empty limiter", waited)
	}
	// The held slot is the only one: another hold cannot fit (queues → ctx error).
	qctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, _, _, err := lim.AcquireHold(qctx); err == nil {
		t.Fatal("hold succeeded while the single slot was held")
	}
	// Under the hold context, per-call acquire skips the limiter (the hold IS the slot).
	if !lim.acquire(hctx) {
		t.Fatal("acquire under hold context failed")
	}
	release() // release the hold; the per-call slot is the caller's to release
	// The slot is free again.
	if !lim.acquire(ctx) {
		t.Fatal("acquire failed after hold release")
	}
}

// AcquireHold respects ctx cancellation while queueing.
func TestHoldRespectsContextCancellation(t *testing.T) {
	p := NewLimiter(stubProvider{}, "test", 1)
	lim := p.(*Limiter)
	ctx := context.Background()
	_, release, _, err := lim.AcquireHold(ctx)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer release()
	qctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, _, err := lim.AcquireHold(qctx); err == nil {
		t.Fatal("expected queueing hold to fail on a full limiter")
	}
}

type stubProvider struct{}

func (stubProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (stubProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "", llm.Usage{}, nil
}
