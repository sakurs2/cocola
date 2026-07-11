package traceevent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingStore struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	partial []string
}

func (s *blockingStore) UpsertConversationRun(context.Context, Run) error { return nil }

func (s *blockingStore) UpsertConversationTraceSpan(context.Context, Span) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}

func (s *blockingStore) MarkConversationRunPartial(_ context.Context, traceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial = append(s.partial, traceID)
	return nil
}

func TestAsyncStoreMarksRunPartialWhenQueueIsFull(t *testing.T) {
	delegate := &blockingStore{started: make(chan struct{}), release: make(chan struct{})}
	store := NewAsyncStore(delegate, 1, nil)

	if err := store.UpsertConversationTraceSpan(context.Background(), Span{TraceID: "trace-1"}); err != nil {
		t.Fatalf("enqueue first span: %v", err)
	}
	select {
	case <-delegate.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := store.UpsertConversationTraceSpan(context.Background(), Span{TraceID: "trace-1"}); err != nil {
		t.Fatalf("enqueue buffered span: %v", err)
	}
	if err := store.UpsertConversationTraceSpan(context.Background(), Span{TraceID: "trace-1"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	delegate.mu.Lock()
	partial := append([]string(nil), delegate.partial...)
	delegate.mu.Unlock()
	if len(partial) != 1 || partial[0] != "trace-1" {
		t.Fatalf("partial marks = %v", partial)
	}

	close(delegate.release)
	store.Close()
}
