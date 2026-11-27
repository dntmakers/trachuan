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
	"time"

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
	d.NoClassificationSet = false
	d.ClassificationSetSHA256 = frozen
	return d
}

func TestStart_ClassificationChoiceRefusesWithoutCreatingRun(t *testing.T) {
	ctx := context.Background()
	for _, both := range []bool{false, true} {
		d := lifecycleDeps(t)
		d.NoClassificationSet = both
		if both {
			d.ClassificationSetSHA256 = strings.Repeat("a", 64)
		}
		var before int64
		var called bool
		if err := d.DB.QueryRow(ctx, `SELECT last_value, is_called FROM run_id_seq`).Scan(&before, &called); err != nil {
			t.Fatal(err)
		}
		if err := Start(ctx, d, "vn", d.TaskQueue, "", ""); err == nil || !strings.Contains(err.Error(), "--no-classification-set") {
			t.Fatalf("both=%v error=%v", both, err)
		}
		var count int
		if err := d.DB.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("both=%v persisted %d runs: %v", both, count, err)
		}
		var after int64
		var afterCalled bool
		if err := d.DB.QueryRow(ctx, `SELECT last_value, is_called FROM run_id_seq`).Scan(&after, &afterCalled); err != nil || after != before || afterCalled != called {
			t.Fatalf("both=%v sequence before=(%d,%v) after=(%d,%v) err=%v", both, before, called, after, afterCalled, err)
		}
	}
}

func TestStart_ClassificationChoiceDoesNotWaitOnSingleKeyLock(t *testing.T) {
	ctx := context.Background()
	d := lifecycleDeps(t)
	d.NoClassificationSet = false
	conn, err := d.DB.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	key := "corpus:start:vn:" + d.TaskQueue
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext($1))`, key); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, key) }()
	startCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = Start(startCtx, d, "vn", d.TaskQueue, "", "")
	if err == nil || !strings.Contains(err.Error(), "--no-classification-set") {
		t.Fatalf("start error = %v", err)
	}
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
	var sequenceBefore int64
	if err := d.DB.QueryRow(ctx, `SELECT last_value FROM run_id_seq`).Scan(&sequenceBefore); err != nil {
		t.Fatal(err)
	}
	d.ClassificationSetSHA256 = strings.Repeat("b", 64)
	if err := Start(ctx, d, "vn", d.TaskQueue, "", ""); err == nil || !strings.Contains(err.Error(), "frozen classification set") {
		t.Fatalf("replacement error=%v", err)
	}
	var sequenceAfter int64
	if err := d.DB.QueryRow(ctx, `SELECT last_value FROM run_id_seq`).Scan(&sequenceAfter); err != nil || sequenceBefore != sequenceAfter {
		t.Fatalf("sequence before=%d after=%d err=%v", sequenceBefore, sequenceAfter, err)
	}
	d.ClassificationSetSHA256 = ""
	d.TaxonomyVersion = strings.Repeat("c", 64)
	if err := Start(ctx, d, "vn", d.TaskQueue, "", ""); err != nil {
		t.Fatal(err)
	}
	assertFrozen()
}
