package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockingRollback struct{ remaining chan time.Duration }

func (b blockingRollback) Rollback(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		b.remaining <- 0
		return errors.New("no deadline")
	}
	b.remaining <- time.Until(deadline)
	<-ctx.Done()
	return ctx.Err()
}

func TestRollback_BoundedAfterCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remaining := make(chan time.Duration, 1)
	done := make(chan struct{})
	go func() { defer close(done); rollback(ctx, blockingRollback{remaining: remaining}) }()
	if d := <-remaining; d <= 0 || d > 100*time.Millisecond {
		t.Fatalf("rollback deadline %v", d)
	}
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("rollback did not end")
	}
}
