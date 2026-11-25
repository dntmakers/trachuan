package retrieval

import (
	"context"
	"time"
)

const rollbackTimeout = 50 * time.Millisecond

type rollbacker interface {
	Rollback(context.Context) error
}

func rollback(ctx context.Context, tx rollbacker) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
