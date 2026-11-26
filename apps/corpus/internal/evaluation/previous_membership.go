package evaluation

import "fmt"

func validatePreviousOldProduct(r *Report, suite Suite) error {
	rejected := rejectionSet(suite)
	expected := map[string]int{}
	positive, negative, citations := 0, 0, 0
	for _, c := range suite.Legacy {
		if rejected[c.ID] {
			continue
		}
		expected[c.ID] = len(c.ExpectedCitations)
		if c.ExpectAbstain {
			negative++
		} else {
			positive++
			citations += len(c.ExpectedCitations)
		}
	}
	cohort := r.Cohorts["old_product"]
	count := len(expected)
	if r.Version < 5 {
		count += len(rejected)
	}
	if cohort.CaseCount != count || cohort.PositiveCount != positive || cohort.NegativeCount != negative || cohort.ExpectedCitations != citations {
		return fmt.Errorf("previous old product counts differ from fixture")
	}
	seen := map[string]uint8{}
	for _, row := range r.Cases {
		if row.Cohort != "old_product" {
			continue
		}
		count, ok := expected[row.CaseID]
		if !ok || row.Expected != count || len(row.Ranks) != count {
			return fmt.Errorf("previous old product cases differ from fixture")
		}
		if row.Arms != "hybrid" {
			continue
		}
		var bit uint8
		switch {
		case row.Mode == "exact" && row.Limit == 8:
			bit = 1
		case row.Mode == "exact" && row.Limit == 20:
			bit = 2
		case row.Mode == "ann" && row.Limit == 8:
			bit = 4
		case row.Mode == "ann" && row.Limit == 20:
			bit = 8
		}
		if bit != 0 && seen[row.CaseID]&bit != 0 {
			return fmt.Errorf("previous old product hybrid row duplicated")
		}
		seen[row.CaseID] |= bit
	}
	for id := range expected {
		if seen[id] != 15 {
			return fmt.Errorf("previous old product hybrid membership differs from fixture")
		}
	}
	return nil
}
