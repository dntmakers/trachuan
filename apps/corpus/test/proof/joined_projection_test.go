//go:build integration && faultinject

package proof

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func joinedArtifact(t *testing.T, h *harness, name, file string) string {
	t.Helper()
	var path string
	if err := h.db.QueryRow(h.ctx, `SELECT a.path FROM artifact a JOIN run_item i ON i.artifact_id=a.id JOIN run r ON r.id=i.run_id WHERE r.country='vn' AND r.rel=$1 AND i.stage=$2 AND i.state='done'`, h.rel, name).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(h.artifacts, path, file)
}

func TestProof_JoinedProjection(t *testing.T) {
	start := time.Now()
	reserved := testpg.Release(t)
	t.Run("pair", func(t *testing.T) {
		h := newHarness(t)
		successor := h.rel
		if reserved > successor {
			reserved, successor = successor, reserved
		}
		h.rel = reserved
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		h.ctx = ctx
		h.indexSetup()
		previous := h.rel
		h.previousReport = joinedArtifact(t, h, "eval", "report.json")
		joinedArtifact(t, h, "evidence", "identity.jsonl")
		h.stopWorker()
		base := h.gateway.server
		changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/doc/all" {
				items := make([]map[string]any, 0, 12)
				for i := 1; i <= 13; i++ {
					agency := "62"
					if i == 2 {
						agency = "63"
					}
					items = append(items, map[string]any{"id": fmt.Sprint(i), "docNum": fmt.Sprintf("%d/2026", i), "title": "Document", "agencyIds": []string{agency}})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"total": len(items), "items": items}})
				return
			}
			if r.URL.Path == "/doc/provision/tree/1" {
				rec := httptest.NewRecorder()
				base.Config.Handler.ServeHTTP(rec, r)
				_, _ = w.Write([]byte(strings.ReplaceAll(rec.Body.String(), "Thông tư này quy định", "Thông tư này sửa đổi quy định")))
				return
			}
			if r.URL.Path == "/doc/1" {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "1", "docNum": "1/2026", "title": "Document 1", "hasContent": true, "effTo": "2099-01-01"}})
				return
			}
			base.Config.Handler.ServeHTTP(w, r)
		}))
		t.Cleanup(changed.Close)
		h.gateway.server = changed
		h.rel = successor
		h.indexSetup()
		buildPath := joinedArtifact(t, h, "build", "build.json")
		changesPath := joinedArtifact(t, h, "evidence", "changes.jsonl")
		data, err := os.ReadFile(buildPath)
		if err != nil {
			t.Fatal(err)
		}
		var record build.BuildRecord
		if err = json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		var builderURL string
		for _, v := range h.extra {
			if strings.HasPrefix(v, "CORPUS_BUILDER_DATABASE_URL=") {
				builderURL = strings.TrimPrefix(v, "CORPUS_BUILDER_DATABASE_URL=")
			}
		}
		admin, err := pgx.Connect(ctx, builderURL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = admin.Close(context.Background()) }()
		if !testpg.DropCorpusDatabase(t, admin, "corpus_vn_"+h.rel, "vn/"+h.rel) {
			t.Fatal("drop built database")
		}
		h.command("eval", "vn", h.rel, "--out", filepath.Join(h.root, "restored-eval"))
		cfg, err := pgxpool.ParseConfig(builderURL)
		if err != nil {
			t.Fatal(err)
		}
		cfg.ConnConfig.Database = "corpus_vn_" + h.rel
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var markerJSON string
		if err = pool.QueryRow(ctx, `SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname=current_database()`).Scan(&markerJSON); err != nil {
			t.Fatal(err)
		}
		var marker struct {
			Instance string `json:"instance"`
		}
		if err = json.Unmarshal([]byte(markerJSON), &marker); err != nil || marker.Instance != "restored" {
			t.Fatalf("database marker %s: %v", markerJSON, err)
		}
		var storedRows []releasechanges.Row
		stored, err := corpusdb.ReadReleaseChanges(ctx, pool, corpusdb.ReleaseChangeLimits(), func(row releasechanges.Row) error { storedRows = append(storedRows, row); return nil })
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(changesPath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		hasher, err := releasechanges.NewHasher(stored.Header)
		if err != nil {
			t.Fatal(err)
		}
		var sidecarRows []releasechanges.Row
		if err = releasechange.ProjectStream(file, func(row releasechanges.Row) error { sidecarRows = append(sidecarRows, row); return hasher.Add(row) }); err != nil {
			t.Fatal(err)
		}
		if stored.Digest != hasher.Sum() || stored.Digest != record.ChangesProjection.SHA256 || stored.Header.Version != record.ChangesProjection.Version || !maps.Equal(stored.Counts, hasher.Counts()) || !maps.Equal(stored.Counts, record.ChangesCounts) || !reflect.DeepEqual(storedRows, sidecarRows) {
			t.Fatalf("projection mismatch stored=%+v sidecar=%s build=%+v", stored, hasher.Sum(), record.ChangesProjection)
		}
		if stored.Header.Predecessor == nil || *stored.Header.Predecessor != previous {
			t.Fatalf("wrong predecessor: %+v", stored.Header)
		}
		wantCounts := map[string]int{"document_added": 1, "document_removed": 1, "document_validity": 1, "provision_validity": 0, "provision_text": 2, "document_key": 0}
		if !maps.Equal(stored.Counts, wantCounts) {
			t.Fatalf("counts=%v, want %v", stored.Counts, wantCounts)
		}
		root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "deploy", "github"))
		if err != nil {
			t.Fatal(err)
		}
		script := `import json, sys
sys.path.insert(0, sys.argv[1])
from corpus_projection import project
with open(sys.argv[2], 'rb') as f:
 p = project(f, 'vn')
print(json.dumps({'digest': p.digest, 'counts': p.counts}))`
		out, err := exec.CommandContext(ctx, "python3", "-I", "-c", script, root, changesPath).CombinedOutput()
		if err != nil {
			t.Fatalf("python projection: %v: %s", err, out)
		}
		var python struct {
			Digest string         `json:"digest"`
			Counts map[string]int `json:"counts"`
		}
		if err = json.Unmarshal(out, &python); err != nil || python.Digest != stored.Digest || !maps.Equal(python.Counts, stored.Counts) {
			t.Fatalf("python projection: %s: %v", out, err)
		}
		result, err := pool.Exec(ctx, `UPDATE release_change SET payload=replace(payload, '"2099-01-01"', '"2099-02-01"') WHERE kind='document_validity' AND payload::jsonb #>> '{eff_to,after}' = '2099-01-01'`)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("tamper: %v rows=%d", err, result.RowsAffected())
		}
		_, err = corpusdb.ReadReleaseChanges(ctx, pool, corpusdb.ReleaseChangeLimits(), nil)
		var invalid *corpusdb.InvalidProjectionError
		if !errors.As(err, &invalid) {
			t.Fatalf("tamper error: %v", err)
		}
		t.Logf("joined pair runtime %s, counts %v", time.Since(start), stored.Counts)
	})
}
