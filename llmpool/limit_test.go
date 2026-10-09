package llmpool

import (
	"context"
	"errors"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

// capFake records the peak number of concurrent in-flight calls and can delay
// each call long enough for concurrency to overlap.
type capFake struct {
	mu        sync.Mutex
	inFlight  int
	peak      int
	delay     time.Duration
	streaming bool
}

func (f *capFake) observe() func() {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.peak {
		f.peak = f.inFlight
	}
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}
}

func (f *capFake) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		defer f.observe()()
		time.Sleep(f.delay)
		yield(llm.StreamEvent{Type:llm.SETextDelta, Text: "ok"}, nil)
	}
}

func (f *capFake) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	defer f.observe()()
	time.Sleep(f.delay)
	return llm.Message{Role: "assistant", Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "ok"}}}, "", llm.Usage{}, nil
}

// drainCap consumes a stream to completion.
func drainCap(seq iter.Seq2[llm.StreamEvent, error]) {
	for _, err := range seq {
		if err != nil {
			return
		}
	}
}

func TestNewLimiterIdentity(t *testing.T) {
	var p llm.Provider = &capFake{}
	if got := NewLimiter(p, "x", 0); got != p {
		t.Fatal("max 0 must return the provider unchanged")
	}
	if got := NewLimiter(p, "x", -3); got != p {
		t.Fatal("negative max must return the provider unchanged")
	}
	if got := NewLimiter(nil, "x", 4); got != nil {
		t.Fatal("nil provider must return nil")
	}
}

func TestLimiterCapsConcurrentComplete(t *testing.T) {
	fake := &capFake{delay: 60 * time.Millisecond}
	const capN = 2
	lim := NewLimiter(fake, "dgx", capN)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _ = lim.Complete(context.Background(), llm.CompletionRequest{})
		}()
	}
	wg.Wait()

	fake.mu.Lock()
	peak := fake.peak
	fake.mu.Unlock()
	if peak > capN {
		t.Fatalf("peak in-flight %d exceeds cap %d", peak, capN)
	}
	if peak < 2 {
		t.Fatalf("peak in-flight %d: limiter serialized requests that should overlap", peak)
	}
}

func TestLimiterCapsConcurrentStream(t *testing.T) {
	fake := &capFake{delay: 60 * time.Millisecond, streaming: true}
	const capN = 3
	lim := NewLimiter(fake, "dgx", capN)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			drainCap(lim.Stream(context.Background(), llm.CompletionRequest{}))
		}()
	}
	wg.Wait()

	fake.mu.Lock()
	peak := fake.peak
	fake.mu.Unlock()
	if peak > capN {
		t.Fatalf("peak in-flight %d exceeds cap %d", peak, capN)
	}
}

func TestLimiterQueueOrderAndThroughput(t *testing.T) {
	// With cap 1 and no delay, queued calls still all complete (no deadlock,
	// no dropped work) — the regression case for a botched release path.
	lim := NewLimiter(&capFake{}, "dgx", 1)
	var done atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _ = lim.Complete(context.Background(), llm.CompletionRequest{})
			done.Add(1)
		}()
	}
	wg.Wait()
	if done.Load() != 16 {
		t.Fatalf("completed %d/16 requests", done.Load())
	}
}

func TestLimiterContextCancelWhileQueued(t *testing.T) {
	// Occupy the only slot, then cancel a queued caller: it must return the
	// context error instead of waiting forever, and the slot must free up.
	release := make(chan struct{})
	blocking := &oneShotAtATime{block: release}
	lim := NewLimiter(blocking, "dgx", 1)

	occupierDone := make(chan struct{})
	go func() { // occupier: takes the only slot and holds it
		defer close(occupierDone)
		_, _, _, _ = lim.Complete(context.Background(), llm.CompletionRequest{})
	}()
	time.Sleep(30 * time.Millisecond) // let the occupier take the slot

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { // queued caller: waits for a slot that never comes, then cancelled
		_, _, _, err := lim.Complete(ctx, llm.CompletionRequest{})
		errCh <- err
	}()
	time.Sleep(30 * time.Millisecond) // let the queued call block on the slot
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued call returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued call did not return after cancel")
	}
	close(release) // unblock the occupier so the goroutine can exit
	<-occupierDone
}

// oneShotAtATime blocks every call until block is closed.
type oneShotAtATime struct{ block chan struct{} }

func (o *oneShotAtATime) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		<-o.block
		yield(llm.StreamEvent{Type:llm.SETextDelta, Text: "late"}, nil)
	}
}

func (o *oneShotAtATime) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	<-o.block
	return llm.Message{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "late"}}}, "", llm.Usage{}, nil
}
