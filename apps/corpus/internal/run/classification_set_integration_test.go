//go:build integration

package run

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func classificationLifecycleDeps(t *testing.T) Deps {
	t.Helper()
	d := lifecycleDeps(t)
	d.Artifacts = artifact.Store{Root: t.TempDir()}
	d.TaxonomyVersion = strings.Repeat("a", 64)
	d.PreviousReport = ReportChoice{None: true}
	d.Stages = append(d.Stages, "build")
	baseRecipes := d.RecipesFor
	d.RecipesFor = func(snapshot stage.Snapshot) map[string]string {
		recipes := baseRecipes(snapshot)
		recipes["build"] = "legacy-build"
		if snapshot.ClassificationSetSHA256 != "" {
			recipes["build"] = "classification-build"
		}
		return recipes
	}
	policy := classify.ClassificationPolicy{Article: json.RawMessage(`{}`)}
	tuple, err := json.Marshal([3]any{policy.Document, policy.Article, policy.Menu})
	if err != nil {
		t.Fatal(err)
	}
	set := classify.ClassificationSet{Version: 1, Source: classify.ClassificationBuild{Country: "vn"}, Taxonomy: d.TaxonomyVersion, Policy: policy, PolicyHash: fmt.Sprintf("%x", sha256.Sum256(tuple))}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	frozen := fmt.Sprintf("%x", sha256.Sum256(raw))
	dir := d.Artifacts.Abs(filepath.Join(classify.ClassificationSetNamespace, frozen))
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, classify.ClassificationSetFilename), raw, 0600); err != nil {
		t.Fatal(err)
	}
	d.ClassificationSetSHA256 = frozen
	return d
}

func TestStart_ClassificationSetRequiresFrozenBuildStage(t *testing.T) {
	ctx := context.Background()
	d := classificationLifecycleDeps(t)
	if err := Start(ctx, d, "vn", d.TaskQueue, "discover", ""); err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatalf("early selection error=%v", err)
	}
	var count int
	if err := d.DB.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("early selection persisted %d runs: %v", count, err)
	}
}

func TestStart_FreezesClassificationSetAcrossRestart(t *testing.T) {
	ctx := context.Background()
	d := classificationLifecycleDeps(t)
	frozen := d.ClassificationSetSHA256
	if err := Start(ctx, d, "vn", d.TaskQueue, "build", ""); err != nil {
		t.Fatal(err)
	}
	assertFrozen := func() {
		t.Helper()
		var snapshot stage.Snapshot
		if e := d.DB.QueryRow(ctx, `SELECT snapshot FROM run`).Scan(&snapshot); e != nil || snapshot.ClassificationSetSHA256 != frozen || snapshot.Recipes["build"] != "classification-build" {
			t.Fatalf("snapshot digest=%q recipe=%q err=%v", snapshot.ClassificationSetSHA256, snapshot.Recipes["build"], e)
		}
	}
	assertFrozen()
	if _, err := d.DB.Exec(ctx, `UPDATE run SET state='paused'`); err != nil {
		t.Fatal(err)
	}
	if err := d.Temporal.TerminateWorkflow(ctx, WorkflowID("vn", d.TaskQueue), "", "test resume"); err != nil {
		t.Fatal(err)
	}
	d.ClassificationSetSHA256 = strings.Repeat("b", 64)
	if err := Start(ctx, d, "vn", d.TaskQueue, "", ""); err == nil || !strings.Contains(err.Error(), "frozen classification set") {
		t.Fatalf("replacement error=%v", err)
	}
	d.ClassificationSetSHA256 = ""
	d.TaxonomyVersion = strings.Repeat("c", 64)
	if err := Start(ctx, d, "vn", d.TaskQueue, "", ""); err != nil {
		t.Fatal(err)
	}
	assertFrozen()
}
