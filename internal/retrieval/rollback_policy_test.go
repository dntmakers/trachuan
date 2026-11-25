package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rollbackDeadlineProbe struct {
	pgx.Tx
	deadline chan time.Time
}

func (p rollbackDeadlineProbe) Rollback(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	p.deadline <- deadline
	return nil
}
func (p rollbackDeadlineProbe) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("setup failure")
}

func TestRollback_OrdinaryBudget(t *testing.T) {
	ch := make(chan time.Time, 1)
	start := time.Now()
	rollback(context.Background(), rollbackDeadlineProbe{deadline: ch})
	d := (<-ch).Sub(start)
	if d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Fatalf("rollback budget %v", d)
	}
}

func TestMetadataTx_SetupUsesSampleRollbackDeadline(t *testing.T) {
	ch := make(chan time.Time, 1)
	deadline := time.Now().Add(50 * time.Millisecond)
	ctx := WithRollbackDeadline(context.Background(), deadline)
	_ = configureMetadataTx(ctx, rollbackDeadlineProbe{deadline: ch}, 2*time.Second)
	got := <-ch
	if !got.Equal(deadline) {
		t.Fatalf("rollback deadline %v, want %v", got, deadline)
	}
}
