package build

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"github.com/jackc/pgx/v5"
)

func verifyClassificationRecord(store artifact.Store, run stage.Run, record *BuildRecord) error {
	if record.ClassificationSetSHA256 != run.Snapshot.ClassificationSetSHA256 {
		return fmt.Errorf("build classification set differs from frozen run input")
	}
	if record.ClassificationSetSHA256 == "" {
		if record.ClassificationPolicySHA256 != "" || (record.Taxonomy != nil && record.Taxonomy.ClassificationStats != nil) {
			return fmt.Errorf("classification policy or counts without a set")
		}
		return nil
	}
	set, err := readBuildClassification(context.Background(), store, run, record.TaxonomyVersion)
	if err != nil {
		return err
	}
	if record.ClassificationPolicySHA256 != set.PolicyHash || record.TaxonomyVersion != set.Taxonomy || record.Taxonomy == nil || record.Taxonomy.ClassificationStats == nil {
		return fmt.Errorf("build classification policy, taxonomy or counts mismatch")
	}
	stats := record.Taxonomy.ClassificationStats
	if stats.LLMAssignedArticles < 0 || stats.LLMRows < stats.LLMAssignedArticles || stats.LLMUnusedArticles < 0 || stats.LLMGaps == nil {
		return fmt.Errorf("invalid build classification counts")
	}
	remaining := int64(len(set.Articles)) - stats.LLMAssignedArticles
	for reason, count := range stats.LLMGaps {
		if !classificationGapReason(reason) || count <= 0 {
			return fmt.Errorf("invalid build classification gap counts")
		}
		if reason != "missing_result" && reason != "changed_own_text" {
			if count > remaining {
				return fmt.Errorf("build classification uses more records than the set contains")
			}
			remaining -= count
		}
	}
	if remaining != stats.LLMUnusedArticles {
		return fmt.Errorf("build classification unused article count mismatch")
	}
	record.classification = set
	return nil
}

func classificationGapReason(reason string) bool {
	switch reason {
	case "missing_result", "changed_own_text", "no_family", "invalid_response", "too_long", "no_code":
		return true
	default:
		return false
	}
}

func validateClassificationDatabase(ctx context.Context, db *pgx.Conn, record BuildRecord) error {
	assignments, count, err := readClassificationAssignments(ctx, db, record.TaxonomyVersion)
	if err != nil {
		return err
	}
	gaps, reasons, err := readClassificationGaps(ctx, db)
	if err != nil {
		return err
	}
	stats := record.Taxonomy.ClassificationStats
	if record.ClassificationSetSHA256 == "" {
		if stats != nil || count != 0 || len(gaps) != 0 {
			return fmt.Errorf("classification coverage without a set: %w", errInvalidDatabase)
		}
		return nil
	}
	if stats == nil || count != stats.LLMRows || int64(len(assignments)) != stats.LLMAssignedArticles || !maps.Equal(reasons, stats.LLMGaps) {
		return fmt.Errorf("classification counts mismatch: %w", errInvalidDatabase)
	}
	if record.classification == nil {
		return fmt.Errorf("missing verified classification set: %w", errInvalidDatabase)
	}
	nodes, err := readClassificationNodes(ctx, db)
	if err != nil {
		return err
	}
	matcher, err := newClassificationMatcher(*record.classification, nodes)
	if err != nil {
		return fmt.Errorf("validate classification against database taxonomy: %w: %w", err, errInvalidDatabase)
	}
	if err = classify.VisitEligibleArticles(ctx, db, func(id int64, key, anchor, text string) error {
		identity := classificationArticleKey{key, anchor}
		want, reason := matcher.match(key, anchor, text)
		if !maps.Equal(assignments[id], want) || gaps[identity] != reason {
			return fmt.Errorf("classification differs from frozen evidence at %q %q: %w", key, anchor, errInvalidDatabase)
		}
		delete(assignments, id)
		delete(gaps, identity)
		return nil
	}); err != nil {
		return err
	}
	if len(assignments) != 0 || len(gaps) != 0 {
		return fmt.Errorf("classification coverage on an ineligible article: %w", errInvalidDatabase)
	}
	return nil
}

func readClassificationAssignments(ctx context.Context, db *pgx.Conn, version string) (map[int64]map[int64]float64, int64, error) {
	const query = `SELECT a.provision_id,a.family_id,a.confidence,n.status='active' AND n.level=2 AND a.node_id=a.family_id AND NOT a.exclude AND a.taxonomy_version=$1
 FROM taxonomy_assignment a JOIN taxonomy_node n ON n.id=a.node_id WHERE a.method='llm'`
	rows, err := db.Query(ctx, query, version)
	if err != nil {
		return nil, 0, fmt.Errorf("read LLM rows: %w", err)
	}
	defer rows.Close()
	assignments := map[int64]map[int64]float64{}
	var count int64
	for rows.Next() {
		var id, family int64
		var confidence float64
		var valid bool
		if err = rows.Scan(&id, &family, &confidence, &valid); err != nil {
			return nil, 0, fmt.Errorf("decode LLM rows: %w", err)
		}
		if !valid {
			return nil, 0, fmt.Errorf("invalid LLM row for provision %d: %w", id, errInvalidDatabase)
		}
		if assignments[id] == nil {
			assignments[id] = map[int64]float64{}
		}
		assignments[id][family] = confidence
		count++
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read LLM rows: %w", err)
	}
	return assignments, count, nil
}

func readClassificationGaps(ctx context.Context, db *pgx.Conn) (map[classificationArticleKey]string, map[string]int64, error) {
	rows, err := db.Query(ctx, `SELECT item_key,detail FROM coverage_gap WHERE stage='build' AND kind='unassigned'`)
	if err != nil {
		return nil, nil, fmt.Errorf("read unassigned gaps: %w", err)
	}
	defer rows.Close()
	gaps, counts := map[classificationArticleKey]string{}, map[string]int64{}
	for rows.Next() {
		var key, detail string
		if err = rows.Scan(&key, &detail); err != nil {
			return nil, nil, fmt.Errorf("decode unassigned gap: %w", err)
		}
		anchor, reason, found := strings.Cut(detail, " ")
		identity := classificationArticleKey{key, anchor}
		if !found || anchor == "" || !classificationGapReason(reason) || gaps[identity] != "" {
			return nil, nil, fmt.Errorf("invalid or duplicate unassigned gap for %q %q: %w", key, detail, errInvalidDatabase)
		}
		gaps[identity] = reason
		counts[reason]++
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read unassigned gaps: %w", err)
	}
	return gaps, counts, nil
}

func readClassificationNodes(ctx context.Context, db *pgx.Conn) (map[string]taxonomyNode, error) {
	const query = `SELECT n.id,n.code,n.level,n.status,COALESCE(f.id,0)
 FROM taxonomy_node n LEFT JOIN taxonomy_node f ON f.level=2 AND n.path <@ f.path`
	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read classification taxonomy: %w", err)
	}
	defer rows.Close()
	nodes := map[string]taxonomyNode{}
	for rows.Next() {
		var n taxonomyNode
		if err = rows.Scan(&n.id, &n.code, &n.level, &n.status, &n.family); err != nil {
			return nil, fmt.Errorf("decode classification taxonomy: %w", err)
		}
		nodes[n.code] = n
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read classification taxonomy: %w", err)
	}
	return nodes, nil
}
