//go:build integration

package evidence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func projectionBatchFixture(t *testing.T) (*batchFixture, *pgxpool.Pool) {
	t.Helper()
	frozen := strings.Repeat("a", 64)
	f := newBatchFixture(t, stage.PreviousReport{SHA256: frozen})
	f.addPredecessor(frozen)
	identity := predecessorIdentityBytes(t, previousRel, frozen)
	lock := &stage.LockedPredecessor{ReportSHA256: frozen, Release: previousRel, IdentitySHA256: bareSHA256(identity)}
	f.run.Snapshot.LockedPredecessor = lock
	snapshot, err := json.Marshal(f.run.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE run SET snapshot=$2 WHERE id=$1`, f.run.ID, snapshot)
	pool, err := pgxpool.New(f.ctx, testpg.NewDatabase(t, "vector", "ltree"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = corpusdb.ApplyTables(f.ctx, pool, 1024); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(t.TempDir(), "identity.jsonl")
	file, err := os.Create(current)
	if err != nil {
		t.Fatal(err)
	}
	summary, members := currentSummary()
	if err = releasechange.WriteIdentity(f.ctx, file, summary, members, releasechange.IdentityBindings{Rel: testRel}); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(t.TempDir(), "identity.jsonl")
	if err = os.WriteFile(previous, identity, 0600); err != nil {
		t.Fatal(err)
	}
	header, err := build.ProjectionHeader(f.run)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	changes := releasechange.ChangeInputs{Current: current, Predecessor: previous, Members: members, Bindings: releasechange.ChangesBindings{PredecessorRel: previousRel, PredecessorIdentitySHA256: "sha256:" + lock.IdentitySHA256}}
	projection, err := corpusdb.WriteReleaseChangesStream(f.ctx, tx, header, func(emit func(releasechanges.Row) error) error {
		return releasechange.ProjectRecords(false, func(yield func(any) error) error { return releasechange.Generate(changes, yield) }, emit)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if projection.Counts["document_added"]+projection.Counts["document_removed"] == 0 {
		t.Fatal("fixture has no document changes")
	}
	record, err := json.Marshal(build.BuildRecord{SchemaVersion: 9, ChangesProjection: build.ProjectionDigest{Version: releasechanges.Version, SHA256: projection.Digest}, ChangesCounts: projection.Counts})
	if err != nil {
		t.Fatal(err)
	}
	f.build, err = f.env.Artifacts.Publish(f.ctx, "build", strings.Repeat("9", 64), f.recipes["build"], func(dir string) error { return os.WriteFile(filepath.Join(dir, "build.json"), record, 0600) })
	if err != nil {
		t.Fatal(err)
	}
	var artifactID int64
	if err = f.db.QueryRow(f.ctx, `INSERT INTO artifact(stage,item_key,input_hash,recipe_hash,path,sha256) VALUES('build',$1,$2,$3,$4,$5) RETURNING id`, batchKey, f.build.InputHash, f.recipes["build"], f.build.Path, f.build.SHA256).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE run_item SET artifact_id=$2,input_hash=$3 WHERE run_id=$1 AND stage='build'`, f.run.ID, artifactID, f.build.InputHash)
	f.stage.release = func(context.Context, stage.Env, stage.Run) (*pgxpool.Pool, func(), error) {
		return pool, func() {}, nil
	}
	f.commitEval(frozen, "projection")
	return f, pool
}

func TestEvidenceBatch_ProjectionCompletion(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "tampered"}[tamper], func(t *testing.T) {
			f, pool := projectionBatchFixture(t)
			if tamper {
				if _, err := pool.Exec(f.ctx, `UPDATE release_change SET payload='{}' WHERE ordinal=1`); err != nil {
					t.Fatal(err)
				}
			}
			f.plan()
			result, err := f.batch(f.ctx, f.stage, 1)
			if tamper {
				if err != nil || result.Failed != 1 || f.item().state != "failed" {
					t.Fatalf("result=%+v item=%+v err=%v", result, f.item(), err)
				}
				return
			}
			f.wantDone(result, err)
			var path string
			if err = f.db.QueryRow(f.ctx, `SELECT path FROM artifact WHERE id=$1`, *f.item().artifactID).Scan(&path); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(filepath.Join(f.env.Artifacts.Abs(path), changesFile))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			header, err := build.ProjectionHeader(f.run)
			if err != nil {
				t.Fatal(err)
			}
			h, err := releasechanges.NewHasher(header)
			if err != nil {
				t.Fatal(err)
			}
			if err = releasechange.ProjectStream(file, h.Add); err != nil {
				t.Fatal(err)
			}
			stored, err := corpusdb.ReadReleaseChanges(f.ctx, pool, corpusdb.ReleaseChangeLimits(), nil)
			if err != nil || stored.Digest != h.Sum() {
				t.Fatalf("stored=%+v sidecar=%s err=%v", stored, h.Sum(), err)
			}
		})
	}
}
