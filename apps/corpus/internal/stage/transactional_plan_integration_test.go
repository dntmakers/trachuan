//go:build integration

package stage

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

type transactionalControlStage struct {
	*controlStage
	invalid bool
}

func (s *transactionalControlStage) PlanInTransaction(ctx context.Context, _ Env, tx pgx.Tx, run Run) ([]Item, error) {
	_, err := tx.Exec(ctx, `UPDATE run SET snapshot=jsonb_set(snapshot,'{locked_predecessor}','{"report_sha256":"none","release":"none","identity_sha256":"none"}') WHERE id=$1`, run.ID)
	if err != nil {
		return nil, err
	}
	key := "build"
	if s.invalid {
		key = "\x00"
	}
	return []Item{{Key: key, InputHash: "input"}}, nil
}

func TestPlanStage_TransactionIncludesLockAndItem(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[invalid], func(t *testing.T) {
			env, in, base := controlSetup(t)
			s := &transactionalControlStage{controlStage: base, invalid: invalid}
			_, err := PlanStage(context.Background(), env, s, in, map[string]string{"fetch": "recipe"})
			if (err != nil) != invalid {
				t.Fatalf("plan error = %v", err)
			}
			var locked bool
			var count int
			if queryErr := env.DB.QueryRow(context.Background(), `SELECT snapshot ? 'locked_predecessor', (SELECT count(*) FROM run_item WHERE run_id=run.id) FROM run WHERE id=$1`, in.RunID).Scan(&locked, &count); queryErr != nil {
				t.Fatal(queryErr)
			}
			if locked != !invalid || (count == 1) != !invalid {
				t.Fatalf("lock=%v items=%d", locked, count)
			}
		})
	}
}
