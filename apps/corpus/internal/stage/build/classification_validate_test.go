package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestVerifyClassificationRecord_RequiresPresentNonNullStats(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	policy := classify.ClassificationPolicy{Article: json.RawMessage(`{}`)}
	tuple, err := json.Marshal([3]any{policy.Document, policy.Article, policy.Menu})
	if err != nil {
		t.Fatal(err)
	}
	set := classify.ClassificationSet{Version: 1, Source: classify.ClassificationBuild{Country: "vn"}, Taxonomy: "fixture", Policy: policy, PolicyHash: hashBytes(tuple)}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	digest := hashBytes(raw)
	dir := store.Abs(filepath.Join(classify.ClassificationSetNamespace, digest))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, classify.ClassificationSetFilename), raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := stage.Run{Country: "vn", Snapshot: stage.Snapshot{ClassificationSetSHA256: digest}}
	verify := func(stats map[string]any) error {
		data, err := json.Marshal(map[string]any{"taxonomy": stats, "taxonomy_version": set.Taxonomy, "classification_set_sha256": digest, "classification_policy_sha256": set.PolicyHash})
		if err != nil {
			return err
		}
		var record BuildRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		return verifyClassificationRecord(store, run, &record)
	}
	stats := map[string]any{"llm_rows": 0, "llm_assigned_articles": 0, "llm_unused_articles": 0, "llm_gaps": map[string]int{}}
	if err := verify(stats); err != nil {
		t.Fatalf("explicit zero counts rejected: %v", err)
	}
	for _, field := range []string{"llm_rows", "llm_assigned_articles", "llm_unused_articles", "llm_gaps"} {
		t.Run(field, func(t *testing.T) {
			original := stats[field]
			defer func() { stats[field] = original }()
			delete(stats, field)
			if err := verify(stats); err == nil {
				t.Error("accepted missing field")
			}
			stats[field] = nil
			if err := verify(stats); err == nil {
				t.Error("accepted null field")
			}
		})
	}
}
