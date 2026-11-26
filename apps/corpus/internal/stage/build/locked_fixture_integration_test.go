//go:build integration

package build

import (
	"context"
	"encoding/json"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func planLockedFixture(t *testing.T, f *loadFixture, st *Stage, env stage.Env) []stage.Item {
	t.Helper()
	ctx := context.Background()
	f.run.Snapshot.PreviousReport = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
	snapshot, err := json.Marshal(f.run.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.DB.Exec(ctx, `UPDATE run SET snapshot=$2 WHERE id=$1`, f.run.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	tx, err := env.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	items, err := st.PlanInTransaction(ctx, env, tx, f.run)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err = tx.QueryRow(ctx, `SELECT snapshot FROM run WHERE id=$1`, f.run.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(stored, &f.run.Snapshot); err != nil {
		t.Fatal(err)
	}
	if f.run.Snapshot.LockedPredecessor == nil {
		t.Fatal("build plan did not lock the predecessor")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return items
}
