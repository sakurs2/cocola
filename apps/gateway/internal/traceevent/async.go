package traceevent

import (
	"context"
	"sync"
)

// AsyncStore keeps per-span database latency off the SSE hot path. Run rows
// remain synchronous so child spans from other services cannot race their FK.
type AsyncStore struct {
	delegate Store
	spans    chan Span
	done     chan struct{}
	onError  func(error)
	once     sync.Once
}

func NewAsyncStore(delegate Store, capacity int, onError func(error)) *AsyncStore {
	if capacity <= 0 {
		capacity = 2048
	}
	s := &AsyncStore{
		delegate: delegate,
		spans:    make(chan Span, capacity),
		done:     make(chan struct{}),
		onError:  onError,
	}
	go s.run()
	return s
}

func (s *AsyncStore) UpsertConversationRun(ctx context.Context, run Run) error {
	return s.delegate.UpsertConversationRun(ctx, run)
}

func (s *AsyncStore) MarkConversationRunPartial(ctx context.Context, traceID string) error {
	return s.delegate.MarkConversationRunPartial(ctx, traceID)
}

func (s *AsyncStore) UpsertConversationTraceSpan(_ context.Context, span Span) error {
	select {
	case s.spans <- span:
		return nil
	default:
		if err := s.delegate.MarkConversationRunPartial(context.Background(), span.TraceID); err != nil && s.onError != nil {
			s.onError(err)
		}
		return ErrQueueFull
	}
}

func (s *AsyncStore) run() {
	defer close(s.done)
	for span := range s.spans {
		if err := s.delegate.UpsertConversationTraceSpan(context.Background(), span); err != nil {
			if partialErr := s.delegate.MarkConversationRunPartial(context.Background(), span.TraceID); partialErr != nil && s.onError != nil {
				s.onError(partialErr)
			}
			if s.onError != nil {
				s.onError(err)
			}
		}
	}
}

func (s *AsyncStore) Close() {
	s.once.Do(func() { close(s.spans) })
	<-s.done
}
