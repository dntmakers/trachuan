//go:build integration

package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClassificationGate_ActualRowsAndLegacyReport(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testpg.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE TABLE taxonomy_assignment (method text NOT NULL); INSERT INTO taxonomy_assignment VALUES ('phapdien_seed')`); err != nil {
		t.Fatal(err)
	}
	run := stage.Run{Country: "vn", Rel: "20990102_1"}
	store := artifact.Store{Root: t.TempDir()}
	record := build.BuildRecord{}
	newReport := func() evaluation.Report {
		return evaluation.Report{QualityPass: true, NotRun: map[string]string{"taxonomy": "unavailable"}}
	}
	report := newReport()
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := evaluateTaxonomy(ctx, pool, store, run, record, &report); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(report)
	if err != nil || !bytes.Equal(before, after) || bytes.Contains(after, []byte(`"classification_gate"`)) {
		t.Fatalf("legacy report bytes changed: %s, %v", after, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO taxonomy_assignment VALUES ('llm'),('llm')`); err != nil {
		t.Fatal(err)
	}
	record.Taxonomy = &build.TaxonomyStats{ClassificationStats: &build.ClassificationStats{LLMRows: 2}}
	report = newReport()
	if err := evaluateTaxonomy(ctx, pool, store, run, record, &report); err != nil {
		t.Fatal(err)
	}
	gate := report.ClassificationGate
	if report.QualityPass || gate == nil || gate.Status != "missing" || gate.LLMRows != 2 || len(gate.Reasons) == 0 || report.NotRun["taxonomy"] != "" {
		t.Fatalf("LLM rows without a set: quality_pass=%t, gate=%+v, not_run=%v", report.QualityPass, gate, report.NotRun)
	}
}

func TestClassificationGate_RejectsRowsDifferentFromBuild(t *testing.T) {
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, testpg.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `CREATE TABLE taxonomy_assignment (method text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	selected, policy, taxonomy := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	run := stage.Run{Country: "vn", Rel: "20990102_1", Snapshot: stage.Snapshot{ClassificationSetSHA256: selected}}
	store := artifact.Store{Root: t.TempDir()}
	for _, tc := range []struct {
		name     string
		actual   int
		recorded *build.ClassificationStats
	}{
		{"all rows removed", 0, &build.ClassificationStats{LLMRows: 2}},
		{"some rows removed", 1, &build.ClassificationStats{LLMRows: 2}},
		{"rows added", 3, &build.ClassificationStats{LLMRows: 2}},
		{"unexpected rows on legacy build", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `TRUNCATE taxonomy_assignment`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO taxonomy_assignment SELECT 'llm' FROM generate_series(1, $1::int)`, tc.actual); err != nil {
				t.Fatal(err)
			}
			record := build.BuildRecord{ClassificationSetSHA256: selected, ClassificationPolicySHA256: policy, TaxonomyVersion: taxonomy,
				Taxonomy: &build.TaxonomyStats{ClassificationStats: tc.recorded}}
			target := run
			if tc.recorded == nil {
				record.ClassificationSetSHA256, record.ClassificationPolicySHA256 = "", ""
				target.Snapshot.ClassificationSetSHA256 = ""
			}
			report := evaluation.Report{QualityPass: true}
			if err := evaluateTaxonomy(ctx, pool, store, target, record, &report); err != nil {
				t.Fatal(err)
			}
			gate := report.ClassificationGate
			if report.QualityPass || gate == nil || gate.Status != "invalid" || gate.LLMRows != int64(tc.actual) ||
				gate.ClassificationSetSHA256 != record.ClassificationSetSHA256 || gate.PolicySHA256 != record.ClassificationPolicySHA256 ||
				gate.TaxonomyVersion != taxonomy || len(gate.Reasons) != 1 || !strings.Contains(gate.Reasons[0], "LLM row count differs") {
				t.Fatalf("count mismatch must fail before scoring: quality_pass=%t gate=%+v", report.QualityPass, gate)
			}
		})
	}
}
