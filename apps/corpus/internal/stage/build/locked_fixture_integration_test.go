//go:build integration

package build

import (
	"context"
	"encoding/json"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/membership"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

func freezeFixtureMembership(t *testing.T, f *loadFixture) {
	t.Helper()
	ctx := context.Background()
	var frozen bool
	if err := f.workshop.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_scope WHERE run_id=$1 AND final_snapshot IS NOT NULL)`, f.run.ID).Scan(&frozen); err != nil {
		t.Fatal(err)
	}
	if frozen {
		return
	}
	var sources map[string]json.RawMessage
	if err := json.Unmarshal(f.run.Snapshot.SourceList, &sources); err != nil {
		t.Fatal(err)
	}
	var vbpl map[string]json.RawMessage
	if raw := sources["vbpl"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &vbpl); err != nil {
			t.Fatal(err)
		}
	}
	if vbpl == nil {
		vbpl = make(map[string]json.RawMessage)
	}
	vbpl["selection_version"] = json.RawMessage(`1`)
	value, err := json.Marshal(vbpl)
	if err != nil {
		t.Fatal(err)
	}
	sources["vbpl"] = value
	f.run.Snapshot.SourceList, err = json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	entries := []membership.Entry{{DocumentKey: "vn:vbpl:1", Source: "vbpl", SourceID: "1", Class: "primary", Disposition: "active"}}
	if err = membership.PublishPhase(ctx, store.New(f.workshop), f.run.ID, membership.Final, entries); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch) VALUES($1,'relate','vn:vbpl:1','gap',0) ON CONFLICT DO NOTHING`, f.run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFixtureMembership_FreezesFinalInventory(t *testing.T) {
	f := fixture(t)
	f.indexed(t, "vn:vbpl:1", false)
	f.run.Snapshot.SourceList = []byte(`{"country":"vn","status_codes":{}}`)
	freezeFixtureMembership(t, f)
	inv := f.inventory(t)
	if inv.Version != 2 || inv.FinalMembershipHash == "" || len(inv.Membership.Active) != 1 || inv.Membership.Active[0].DocumentKey != "vn:vbpl:1" {
		t.Fatalf("inventory = %+v", inv)
	}
	if err := reconcileMembership(inv); err != nil {
		t.Fatal(err)
	}
}

func planLockedFixture(t *testing.T, f *loadFixture, st *Stage, env stage.Env) []stage.Item {
	t.Helper()
	ctx := context.Background()
	if f.run.Snapshot.PreviousReport == nil {
		f.run.Snapshot.PreviousReport = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
	}
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
