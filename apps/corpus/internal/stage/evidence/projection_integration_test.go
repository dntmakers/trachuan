//go:build integration

package evidence

import (
	"context"
	"encoding/json"
	"errors"
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

func TestCheckProjection_RejectsStoredCorruption(t *testing.T) {
	for _, kind := range []string{"valid", "row", "record", "missing set"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			pool, err := pgxpool.New(ctx, testpg.NewDatabase(t, "vector", "ltree"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pool.Close)
			if err = corpusdb.ApplyTables(ctx, pool, 1024); err != nil {
				t.Fatal(err)
			}
			run := stage.Run{Country: "vn", Rel: "20990101", Snapshot: stage.Snapshot{LockedPredecessor: &stage.LockedPredecessor{ReportSHA256: "none", Release: "none", IdentitySHA256: "none"}}}
			var sidecar []byte
			var projected []releasechanges.Row
			if kind == "row" {
				run.Rel = "20990102_1"
				run.Snapshot.LockedPredecessor.Release = "20990101_1"
				run.Snapshot.LockedPredecessor.IdentitySHA256 = strings.Repeat("a", 64)
				sidecar, err = os.ReadFile("../releasechange/testdata/changes.jsonl")
				if err != nil {
					t.Fatal(err)
				}
				if err = releasechange.ProjectStream(strings.NewReader(string(sidecar)), func(row releasechanges.Row) error { projected = append(projected, row); return nil }); err != nil {
					t.Fatal(err)
				}
				if len(projected) == 0 {
					t.Fatal("fixture has no projected rows")
				}
			}
			header, err := build.ProjectionHeader(run)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			result, err := corpusdb.WriteReleaseChangesStream(ctx, tx, header, func(emit func(releasechanges.Row) error) error {
				for _, row := range projected {
					if err := emit(row); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			record := build.BuildRecord{SchemaVersion: 9, ChangesProjection: build.ProjectionDigest{Version: 1, SHA256: result.Digest}, ChangesCounts: result.Counts}
			if kind == "record" {
				record.ChangesProjection.SHA256 = strings.Repeat("0", 64)
			}
			dir := t.TempDir()
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "build.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if sidecar == nil {
				raw, err = json.Marshal(releasechange.ChangesHeader{Schema: 1, Baseline: true})
				if err != nil {
					t.Fatal(err)
				}
				sidecar = append(raw, '\n')
			}
			if err = os.WriteFile(filepath.Join(dir, changesFile), sidecar, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "row" {
				if _, err = pool.Exec(ctx, `UPDATE release_change SET payload='{}' WHERE ordinal=1`); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "missing set" {
				if _, err = pool.Exec(ctx, `DELETE FROM release_change_set`); err != nil {
					t.Fatal(err)
				}
			}
			s := New(Config{})
			s.release = func(context.Context, stage.Env, stage.Run) (*pgxpool.Pool, func(), error) {
				return pool, func() {}, nil
			}
			err = s.checkProjection(ctx, stage.Env{}, run, frozenInputs{buildDir: dir}, dir)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var refusal *releasechange.InputError
			if !errors.As(err, &refusal) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
