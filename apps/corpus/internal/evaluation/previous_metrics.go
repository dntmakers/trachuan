package evaluation

import (
	"fmt"

	"danny.vn/trachuan/internal/retrieval"
)

func validatePreviousComparisons(r *Report, modes []retrieval.Mode) error {
	if len(modes) == 0 {
		modes = []retrieval.Mode{"exact", "ann"}
	}
	for _, cohort := range []string{"old_product", "development"} {
		if r.Cohorts[cohort].CaseCount == 0 {
			continue
		}
		for _, mode := range modes {
			for _, spec := range []struct {
				name  string
				limit int
			}{{"recall", 20}, {"mrr", 8}} {
				var metric Metric
				count := 0
				for _, item := range r.Cohorts[cohort].Metrics {
					if item.Mode == mode && item.Arms == "hybrid" && item.Name == spec.name && item.Limit == spec.limit {
						metric = item
						count++
					}
				}
				seen := map[string]bool{}
				numerator, denominator := 0.0, 0
				for _, item := range r.Cases {
					if item.Cohort != cohort || item.Mode != mode || item.Arms != "hybrid" || item.Limit != spec.limit {
						continue
					}
					if item.CaseID == "" || seen[item.CaseID] || item.Expected < 0 || len(item.Ranks) != item.Expected || len(item.MappingIssues) != 0 {
						return fmt.Errorf("invalid previous %s %s cases", cohort, spec.name)
					}
					seen[item.CaseID] = true
					if item.Expected == 0 {
						continue
					}
					best := 0
					for _, rank := range item.Ranks {
						if rank < 0 {
							return fmt.Errorf("invalid previous %s %s rank", cohort, spec.name)
						}
						if spec.name == "recall" && rank > 0 && rank <= spec.limit {
							numerator++
						}
						if rank > 0 && rank <= 8 && (best == 0 || rank < best) {
							best = rank
						}
					}
					if spec.name == "recall" {
						denominator += item.Expected
					} else {
						denominator++
						if best > 0 {
							numerator += 1 / float64(best)
						}
					}
				}
				if count != 1 || denominator == 0 || metric.Denominator != denominator || metric.Numerator != numerator || !finite(metric.Value) || *metric.Value != numerator/float64(denominator) {
					return fmt.Errorf("invalid previous %s %s %s metric", cohort, mode, spec.name)
				}
			}
		}
	}
	return nil
}
