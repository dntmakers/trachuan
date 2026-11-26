package evaluation

import (
	"fmt"
	"math"
	"reflect"

	"danny.vn/trachuan/internal/retrieval"
)

func validateCutoverPrevious(r *Report, suite Suite) error {
	rejects := rejectionSet(suite)
	if err := validatePreviousOldProduct(r, suite); err != nil {
		return err
	}
	if err := validatePreviousPrecisions(r); err != nil {
		return err
	}
	if r.Version >= 5 {
		if r.Rejections == nil || len(r.Rejections) != 0 {
			return fmt.Errorf("version 5 previous report has rejection rows")
		}
		for _, cohort := range r.Cohorts {
			if cohort.RejectionCount != 0 {
				return fmt.Errorf("version 5 previous report has rejection counts")
			}
		}
		return nil
	}
	if len(r.Rejections) != len(rejects) || r.Cohorts["old_product"].RejectionCount != len(rejects) {
		return fmt.Errorf("previous rejection count differs from fixture")
	}
	seen := map[string]bool{}
	for _, item := range r.Rejections {
		if !rejects[item.CaseID] || seen[item.CaseID] || !item.Passed || item.Reason != "diacritics_required" {
			return fmt.Errorf("previous rejection differs from fixture")
		}
		seen[item.CaseID] = true
	}
	for name, cohort := range r.Cohorts {
		if name != "old_product" && cohort.RejectionCount != 0 {
			return fmt.Errorf("previous rejection count outside old product")
		}
	}
	return nil
}

func validatePreviousPrecisions(r *Report) error {
	if !reflect.DeepEqual(r.Cohorts["old_product"].Floors, r.Floors) {
		return fmt.Errorf("previous precision floors differ")
	}
	for _, name := range []string{"old_product", "development"} {
		cohort := r.Cohorts[name]
		if name == "development" && cohort.CaseCount == 0 {
			if err := validateEmptyDevelopmentFloors(cohort); err != nil {
				return fmt.Errorf("previous %s: %w", name, err)
			}
			continue
		}
		if err := validatePreviousPrecision(cohort); err != nil {
			return fmt.Errorf("previous %s: %w", name, err)
		}
	}
	return nil
}

func validateEmptyDevelopmentFloors(cohort Cohort) error {
	if len(cohort.Floors) != 2 {
		return fmt.Errorf("empty development precision floors differ")
	}
	for i, mode := range []retrieval.Mode{"exact", "ann"} {
		for _, metric := range cohort.Metrics {
			if metric.Mode == mode && metric.Arms == "hybrid" && metric.Limit == 8 && metric.Name == "current_law_precision" {
				return fmt.Errorf("empty development has precision metric")
			}
		}
		floor := cohort.Floors[i]
		if floor.Mode != mode || floor.Arms != "hybrid" || floor.Limit != 8 || floor.Name != "current_law_precision" || floor.Floor != .99 || floor.Status != "not_run" || floor.Numerator != 0 || floor.Denominator != 0 || floor.Value != nil || floor.Confusion != nil || floor.NonCurrentTail != nil || floor.NotYetEligible || floor.CapOverride != nil || floor.ArticleCapOverride != nil {
			return fmt.Errorf("invalid empty development precision floor")
		}
	}
	return nil
}

func validatePreviousPrecision(cohort Cohort) error {
	if len(cohort.Floors) != 2 {
		return fmt.Errorf("precision floors differ")
	}
	for _, mode := range []retrieval.Mode{"exact", "ann"} {
		var metric Metric
		count := 0
		for _, candidate := range cohort.Metrics {
			if candidate.Mode == mode && candidate.Arms == "hybrid" && candidate.Limit == 8 && candidate.Name == "current_law_precision" {
				metric = candidate
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("%s current law precision metric missing or duplicated", mode)
		}
		if metric.Denominator < 0 || metric.Numerator < 0 || metric.Numerator > float64(metric.Denominator) || math.Trunc(metric.Numerator) != metric.Numerator {
			return fmt.Errorf("invalid %s current law precision", mode)
		}
		status := "unavailable"
		if metric.Denominator == 0 {
			if metric.Value != nil {
				return fmt.Errorf("invalid %s current law precision", mode)
			}
		} else {
			if !finite(metric.Value) || *metric.Value != metric.Numerator/float64(metric.Denominator) {
				return fmt.Errorf("invalid %s current law precision", mode)
			}
			status = "fail"
			if *metric.Value >= .99 {
				status = "pass"
			}
		}
		found := false
		for _, floor := range cohort.Floors {
			if floor.Mode == mode {
				if found || floor.Status != status || floor.Floor != .99 || !reflect.DeepEqual(floor.Metric, metric) {
					return fmt.Errorf("invalid %s precision floor", mode)
				}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("missing %s precision floor", mode)
		}
	}
	return nil
}
