package retrieval

import (
	"context"
	"time"
)

const rollbackTimeout = time.Second

type rollbackDeadlineKey struct{}

func WithRollbackDeadline(ctx context.Context, deadline time.Time) context.Context {
	return context.WithValue(ctx, rollbackDeadlineKey{}, deadline)
}

type rollbacker interface {
	Rollback(context.Context) error
}

func rollback(ctx context.Context, tx rollbacker) {
	deadline := time.Now().Add(rollbackTimeout)
	if limit, ok := ctx.Value(rollbackDeadlineKey{}).(time.Time); ok && limit.Before(deadline) {
		deadline = limit
	}
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
