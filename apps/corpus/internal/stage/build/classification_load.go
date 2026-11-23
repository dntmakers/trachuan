package build

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"github.com/jackc/pgx/v5"
)

type ClassificationStats struct {
	LLMRows             int64            `json:"llm_rows"`
	LLMAssignedArticles int64            `json:"llm_assigned_articles"`
	LLMUnusedArticles   int64            `json:"llm_unused_articles"`
	LLMGaps             map[string]int64 `json:"llm_gaps"`
}

func (l *loader) loadClassification(ctx context.Context, set *classify.ClassificationSet, nodes map[string]taxonomyNode, version string) error {
	if set == nil {
		return nil
	}
	if set.Taxonomy != version || version == "" {
		return fmt.Errorf("classification set taxonomy mismatch")
	}
	matcher, err := newClassificationMatcher(*set, nodes)
	if err != nil {
		return err
	}
	stats := &ClassificationStats{LLMUnusedArticles: int64(len(set.Articles)), LLMGaps: make(map[string]int64)}
	var assignments [][]any
	err = classify.VisitEligibleArticles(ctx, l.tx, func(id int64, key, anchor, text string) error {
		rows, reason := matcher.match(key, anchor, text)
		if reason != "missing_result" && reason != "changed_own_text" {
			stats.LLMUnusedArticles--
		}
		if reason != "" {
			l.addGap("build", key, "unassigned", anchor+" "+reason)
			stats.LLMGaps[reason]++
			return nil
		}
		stats.LLMAssignedArticles++
		for _, family := range slices.Sorted(maps.Keys(rows)) {
			assignments = append(assignments, []any{id, family, family, "llm", false, rows[family], version})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(assignments) > 0 {
		stats.LLMRows, err = l.tx.CopyFrom(ctx, pgx.Identifier{"taxonomy_assignment"}, []string{"provision_id", "node_id", "family_id", "method", "exclude", "confidence", "taxonomy_version"}, pgx.CopyFromRows(assignments))
		if err != nil {
			return fmt.Errorf("insert LLM taxonomy assignments: %w", err)
		}
		if stats.LLMRows != int64(len(assignments)) {
			return fmt.Errorf("inserted %d LLM taxonomy assignments, want %d", stats.LLMRows, len(assignments))
		}
	}
	l.counts.Taxonomy.ClassificationStats = stats
	return nil
}
