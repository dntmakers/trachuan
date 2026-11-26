//go:build integration

package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"danny.vn/trachuan/internal/retrieval"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlan_RejectsSchema9BuildWithoutLock(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	artifacts := artifact.Store{Root: t.TempDir()}
	sha := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	record := map[string]any{"schema_version": 9, "development_cases_sha256": "fixture", "heading_exclusions_sha256": sha("[]"), "heading_scan_sha256": sha("{}"), "durations_ms": map[string]int{"total": 1}, "retrieval_recipe": retrieval.Recipe3()}
	art, err := artifacts.Publish(ctx, "build", strings.Repeat("a", 64), strings.Repeat("b", 64), func(dir string) error {
		files := map[string]any{"build.json": record, "heading-exclusions.json": json.RawMessage(`[]`), "heading-scan.json": json.RawMessage(`{}`), "inputs.json": map[string]any{}, "validity-review.json": json.RawMessage(`{"version":1,"entries":[]}`)}
		for name, value := range files {
			data, e := json.Marshal(value)
			if e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = pool.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn','20990101','running','{}') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var artifactID int64
	if err = pool.QueryRow(ctx, `INSERT INTO artifact(stage,item_key,input_hash,recipe_hash,path,sha256) VALUES('build','vn/20990101',$1,$2,$3,$4) RETURNING id`, strings.Repeat("a", 64), strings.Repeat("b", 64), art.Path, art.SHA256).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,input_hash,recipe_hash,artifact_id) VALUES($1,'build','vn/20990101','done',0,$2,$3,$4)`, id, strings.Repeat("a", 64), strings.Repeat("b", 64), artifactID); err != nil {
		t.Fatal(err)
	}
	run := stage.Run{ID: id, Country: "vn", Rel: "20990101"}
	_, err = New(Config{}).Plan(ctx, stage.Env{DB: pool, Artifacts: artifacts}, run)
	if err == nil || !strings.Contains(err.Error(), "predecessor") {
		t.Fatalf("plan error = %v", err)
	}
}
