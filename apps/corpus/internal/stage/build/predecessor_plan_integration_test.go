//go:build integration

package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/internal/retrieval"
	"github.com/jackc/pgx/v5/pgxpool"
)

func planLockFixture(t *testing.T) (*loadFixture, *Stage, stage.Env) {
	t.Helper()
	f := fixture(t)
	f.indexed(t, "vn:vbpl:1", false)
	f.run.Snapshot.SourceList = []byte(`{"country":"vn","status_codes":{}}`)
	f.run.Snapshot.PreviousReport = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
	inv := f.inventory(t)
	suite := evaluation.Suite{Legacy: []evaluation.Case{{ID: "one", Question: "ngân hàng", ExpectAbstain: true}, {ID: "two", Question: "abc", ExpectAbstain: true}, {ID: "three", Question: "def", ExpectAbstain: true}, {ID: "four", Question: "ghi", ExpectAbstain: true}}, Rejections: []string{"two", "three", "four"}, Hashes: evaluation.FixtureHashes{Legacy: "legacy"}, Map: evaluation.IdentityMap{Version: 1, LegacySHA256: "legacy", IdentityInventoryHash: inv.IdentityHash()}}
	pinTestSuite(t, &suite)
	st := New(Config{Suite: suite})
	f.run.Snapshot.Recipes["build"] = st.Recipe(f.run.Snapshot)
	snapshot, err := json.Marshal(f.run.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(context.Background(), `UPDATE run SET snapshot=$2, next_stage='build' WHERE id=$1`, f.run.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), f.workshop.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return f, st, stage.Env{DB: pool, Artifacts: f.artifacts}
}

func runBuildPlan(ctx context.Context, f *loadFixture, st *Stage, env stage.Env) error {
	_, err := stage.PlanStage(ctx, env, st, stage.StageInput{RunID: f.run.ID, Stage: "build"}, nil)
	return err
}

func TestBuildPlan_PreviousRecipeRefusedWithoutChanges(t *testing.T) {
	f, st, env := planLockFixture(t)
	ctx := context.Background()
	const previousRecipe = "6629d13cd49ef3a4a1a05b2d393862be40e6e7142ee91ede114f360483e6bd5f"
	if previousRecipe == st.Recipe(f.run.Snapshot) {
		t.Fatal("fixture recipe is not older")
	}
	f.run.Snapshot.Recipes["build"] = previousRecipe
	snapshot, err := json.Marshal(f.run.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `UPDATE run SET snapshot=$2 WHERE id=$1`, f.run.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	key := f.run.Country + "/" + f.run.Rel
	if _, err = f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,input_hash,recipe_hash) VALUES($1,'build',$2,'done',0,repeat('0',64),$3)`, f.run.ID, key, previousRecipe); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `INSERT INTO gap(run_id,stage,item_key,kind,detail) VALUES($1,'build',$2,'test','keep')`, f.run.ID, key); err != nil {
		t.Fatal(err)
	}
	query := `SELECT jsonb_build_object('snapshot',snapshot,'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY stage,item_key) FROM run_item i WHERE i.run_id=r.id),'gaps',(SELECT jsonb_agg(to_jsonb(g) ORDER BY stage,item_key) FROM gap g WHERE g.run_id=r.id)) FROM run r WHERE id=$1`
	var before, after []byte
	if err = f.workshop.QueryRow(ctx, query, f.run.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var hasLock bool
	if err = f.workshop.QueryRow(ctx, `SELECT snapshot ? 'locked_predecessor' FROM run WHERE id=$1`, f.run.ID).Scan(&hasLock); err != nil || hasLock {
		t.Fatalf("lock=%v err=%v", hasLock, err)
	}
	err = runBuildPlan(ctx, f, st, env)
	var refusal *stage.RecipeError
	if !errors.As(err, &refusal) || refusal.Stage != "build" {
		t.Fatalf("plan error=%v", err)
	}
	if err = f.workshop.QueryRow(ctx, query, f.run.ID).Scan(&after); err != nil || string(before) != string(after) {
		t.Fatalf("before=%s after=%s error=%v", before, after, err)
	}
}

func TestBuildPlan_CurrentRecipeReplacesOutdatedInputAtBuild(t *testing.T) {
	f, st, env := planLockFixture(t)
	ctx := context.Background()
	if got, want := f.run.Snapshot.Recipes["build"], st.Recipe(f.run.Snapshot); got == "" || got != want {
		t.Fatalf("frozen recipe=%q current=%q", got, want)
	}
	old := strings.Repeat("0", 64)
	if _, err := f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,input_hash) VALUES($1,'build',$2,'done',0,$3)`, f.run.ID, f.run.Country+"/"+f.run.Rel, old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch) VALUES($1,'eval',$2,'done',0)`, f.run.ID, f.run.Country+"/"+f.run.Rel); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workshop.Exec(ctx, `INSERT INTO gap(run_id,stage,item_key,kind,detail) VALUES($1,'eval',$2,'eval_floor_failed','old')`, f.run.ID, f.run.Country+"/"+f.run.Rel); err != nil {
		t.Fatal(err)
	}
	var otherID int64
	if err := f.workshop.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn','20990101_99','running','{}') RETURNING id`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,input_hash) VALUES($1,'build','other','done',0,$2),($1,'eval','other','done',0,NULL),($1,'evidence','other','done',0,NULL)`, otherID, old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workshop.Exec(ctx, `INSERT INTO gap(run_id,stage,item_key,kind,detail) VALUES($1,'eval','other','eval_floor_failed','keep')`, otherID); err != nil {
		t.Fatal(err)
	}
	if err := runBuildPlan(ctx, f, st, env); err != nil {
		t.Fatal(err)
	}
	var otherItems, otherGaps int
	if err := f.workshop.QueryRow(ctx, `SELECT (SELECT count(*) FROM run_item WHERE run_id=$1 AND stage IN ('build','eval','evidence')), (SELECT count(*) FROM gap WHERE run_id=$1)`, otherID).Scan(&otherItems, &otherGaps); err != nil || otherItems != 3 || otherGaps != 1 {
		t.Fatalf("other items=%d gaps=%d error=%v", otherItems, otherGaps, err)
	}
	var hash, state string
	var locked bool
	if err := f.workshop.QueryRow(ctx, `SELECT i.input_hash,i.state,r.snapshot ? 'locked_predecessor' FROM run r JOIN run_item i ON i.run_id=r.id WHERE r.id=$1 AND i.stage='build'`, f.run.ID).Scan(&hash, &state, &locked); err != nil {
		t.Fatal(err)
	}
	var dependents int
	if err := f.workshop.QueryRow(ctx, `SELECT (SELECT count(*) FROM run_item WHERE run_id=$1 AND stage='eval') + (SELECT count(*) FROM gap WHERE run_id=$1 AND stage='eval')`, f.run.ID).Scan(&dependents); err != nil || dependents != 0 {
		t.Fatalf("dependent rows=%d error=%v", dependents, err)
	}
	if hash == old || len(hash) != 64 || state != "pending" || !locked {
		t.Fatalf("hash=%q state=%q locked=%v", hash, state, locked)
	}
}

func TestBuildPlan_RefusesReplacementAfterEvaluation(t *testing.T) {
	for _, state := range []string{"evaluated", "released"} {
		t.Run(state, func(t *testing.T) {
			f, st, env := planLockFixture(t)
			ctx := context.Background()
			lock := `{"report_sha256":"none","release":"none","identity_sha256":"none"}`
			if _, err := f.workshop.Exec(ctx, `UPDATE run SET state=$2,next_stage='',snapshot=jsonb_set(snapshot,'{locked_predecessor}',$3::jsonb) WHERE id=$1`, f.run.ID, state, lock); err != nil {
				t.Fatal(err)
			}
			if _, err := f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,input_hash) SELECT $1,s,$2,'done',0,repeat('0',64) FROM unnest(ARRAY['build','eval','evidence']) s`, f.run.ID, f.run.Country+"/"+f.run.Rel); err != nil {
				t.Fatal(err)
			}
			if _, err := f.workshop.Exec(ctx, `INSERT INTO gap(run_id,stage,item_key,kind,detail) SELECT $1,s,$2,'test','keep' FROM unnest(ARRAY['build','eval','evidence']) s`, f.run.ID, f.run.Country+"/"+f.run.Rel); err != nil {
				t.Fatal(err)
			}
			var before, after []byte
			query := `SELECT jsonb_build_object('lock',snapshot->'locked_predecessor','items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY stage,item_key) FROM run_item i WHERE i.run_id=r.id),'gaps',(SELECT jsonb_agg(to_jsonb(g) ORDER BY stage,item_key) FROM gap g WHERE g.run_id=r.id)) FROM run r WHERE id=$1`
			if err := f.workshop.QueryRow(ctx, query, f.run.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			err := runBuildPlan(ctx, f, st, env)
			var refusal *stage.BuildReplacementError
			if !errors.As(err, &refusal) {
				t.Fatalf("plan error=%v", err)
			}
			if err := f.workshop.QueryRow(ctx, query, f.run.ID).Scan(&after); err != nil || string(before) != string(after) {
				t.Fatalf("before=%s after=%s error=%v", before, after, err)
			}
		})
	}
}

func TestBuildPlan_LockAndItemAtomic(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			f, st, env := planLockFixture(t)
			if fail {
				if _, err := f.workshop.Exec(context.Background(), `ALTER TABLE run_item ADD CONSTRAINT reject_build CHECK (stage <> 'build')`); err != nil {
					t.Fatal(err)
				}
			}
			err := runBuildPlan(context.Background(), f, st, env)
			if (err != nil) != fail {
				t.Fatalf("plan error = %v", err)
			}
			var locked bool
			var count int
			if err = f.workshop.QueryRow(context.Background(), `SELECT snapshot ? 'locked_predecessor', (SELECT count(*) FROM run_item WHERE run_id=$1 AND stage='build') FROM run WHERE id=$1`, f.run.ID).Scan(&locked, &count); err != nil {
				t.Fatal(err)
			}
			if locked != !fail || (count == 1) != !fail {
				t.Fatalf("lock=%v items=%d", locked, count)
			}
		})
	}
}

func TestBuildPlan_RejectsChangedReport(t *testing.T) {
	f, st, env := planLockFixture(t)
	ctx := context.Background()
	if err := runBuildPlan(ctx, f, st, env); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := f.workshop.QueryRow(ctx, `SELECT snapshot->'locked_predecessor' FROM run WHERE id=$1`, f.run.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	recipe := retrieval.Recipe3()
	raw, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprintf(`{"version":2,"status":"complete","country":"vn","rel":"20261118_1","retrieval_recipe":%s,"recipes":{"retrieval":%q}}`, raw, recipe.Hash())
	art, err := f.artifacts.PublishContent(ctx, "previous-report", strings.Repeat("a", 64), func(dir string) error { return os.WriteFile(filepath.Join(dir, "report.json"), []byte(report), 0600) })
	if err != nil {
		t.Fatal(err)
	}
	decision, err := json.Marshal(stage.PreviousReport{SHA256: hashBytes([]byte(report)), Path: art.Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn','20261118_1','evaluated','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `UPDATE run SET snapshot=jsonb_set(snapshot,'{PreviousReport}',$2::jsonb) WHERE id=$1`, f.run.ID, decision); err != nil {
		t.Fatal(err)
	}
	if err = runBuildPlan(ctx, f, st, env); err == nil || !strings.Contains(err.Error(), "locked decision") {
		t.Fatalf("plan error = %v", err)
	}
	var after []byte
	if err = f.workshop.QueryRow(ctx, `SELECT snapshot->'locked_predecessor' FROM run WHERE id=$1`, f.run.ID).Scan(&after); err != nil || string(after) != string(before) {
		t.Fatalf("lock changed: %s -> %s, %v", before, after, err)
	}
}
