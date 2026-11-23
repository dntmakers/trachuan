package build

import (
	"encoding/json"
	"fmt"
)

func (stats *TaxonomyStats) UnmarshalJSON(data []byte) error {
	type taxonomyStats TaxonomyStats
	decoded := struct {
		*taxonomyStats
		Rows     *int64            `json:"llm_rows"`
		Assigned *int64            `json:"llm_assigned_articles"`
		Unused   *int64            `json:"llm_unused_articles"`
		Gaps     *map[string]int64 `json:"llm_gaps"`
	}{taxonomyStats: (*taxonomyStats)(stats)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode taxonomy statistics: %w", err)
	}
	stats.ClassificationStats = nil
	if decoded.Rows == nil && decoded.Assigned == nil && decoded.Unused == nil && decoded.Gaps == nil {
		return nil
	}
	if decoded.Rows == nil || decoded.Assigned == nil || decoded.Unused == nil || decoded.Gaps == nil {
		return fmt.Errorf("classification statistics must all be present and non-null")
	}
	stats.ClassificationStats = &ClassificationStats{
		LLMRows: *decoded.Rows, LLMAssignedArticles: *decoded.Assigned, LLMUnusedArticles: *decoded.Unused, LLMGaps: *decoded.Gaps,
	}
	return nil
}
