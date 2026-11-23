package eval

import (
	"context"
	"errors"
	"fmt"
	"os"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func evaluateTaxonomy(ctx context.Context, pool *pgxpool.Pool, artifacts artifact.Store, run stage.Run, record build.BuildRecord, report *evaluation.Report) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("open taxonomy evaluation snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if record.Taxonomy == nil && run.Snapshot.ClassificationSetSHA256 == "" && record.ClassificationSetSHA256 == "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('taxonomy_assignment') IS NOT NULL`).Scan(&exists); err != nil {
			return fmt.Errorf("check taxonomy assignments: %w", err)
		}
		if !exists {
			return nil
		}
	}
	var rows int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM taxonomy_assignment WHERE method='llm'`).Scan(&rows); err != nil {
		return fmt.Errorf("count llm assignments: %w", err)
	}
	var recordedRows int64
	if record.Taxonomy != nil {
		applyInheritance(report, *record.Taxonomy, rows > 0)
		if stats := record.Taxonomy.ClassificationStats; stats != nil {
			recordedRows = stats.LLMRows
		}
	}
	var gate evaluation.ClassificationGate
	switch {
	case rows != recordedRows:
		gate = initialClassificationGate(run, record)
		gate.Reasons = []string{fmt.Sprintf("LLM row count differs: database has %d, build records %d", rows, recordedRows)}
	case rows == 0:
		return nil
	default:
		gate = readClassificationGate(ctx, tx, artifacts, run, record)
	}
	delete(report.NotRun, "taxonomy")
	gate.LLMRows = rows
	report.ClassificationGate = &gate
	if gate.Status != "pass" {
		report.QualityPass = false
	}
	return nil
}

func initialClassificationGate(run stage.Run, record build.BuildRecord) evaluation.ClassificationGate {
	selected := run.Snapshot.ClassificationSetSHA256
	if selected == "" {
		selected = record.ClassificationSetSHA256
	}
	return evaluation.ClassificationGate{Status: "invalid", ClassificationSetSHA256: selected, PolicySHA256: record.ClassificationPolicySHA256, TaxonomyVersion: record.TaxonomyVersion}
}

func readClassificationGate(ctx context.Context, tx pgx.Tx, artifacts artifact.Store, run stage.Run, record build.BuildRecord) evaluation.ClassificationGate {
	gate := initialClassificationGate(run, record)
	selected := gate.ClassificationSetSHA256
	if selected == "" {
		gate.Status, gate.Reasons = "missing", []string{"LLM rows require a frozen classification set"}
		return gate
	}
	if record.ClassificationSetSHA256 != "" && selected != record.ClassificationSetSHA256 {
		gate.Reasons = []string{"build classification set differs from the frozen run"}
		return gate
	}
	set, err := classify.ReadClassificationSet(ctx, artifacts, selected, run.Country, record.TaxonomyVersion)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			gate.Status = "missing"
		}
		gate.Reasons = []string{err.Error()}
		return gate
	}
	if record.ClassificationPolicySHA256 != "" && record.ClassificationPolicySHA256 != set.PolicyHash {
		gate.Reasons = []string{"classification set policy differs from the build policy"}
		return gate
	}
	gate = classify.ScoreClassificationSet(ctx, tx, set)
	gate.ClassificationSetSHA256 = selected
	return gate
}
