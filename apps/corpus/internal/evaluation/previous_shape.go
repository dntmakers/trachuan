package evaluation

import "fmt"

var (
	comparedCases = map[string]bool{"old_product": true, "development": true, "development_period": true}
)

func validatePreviousShape(root map[string]any) error {
	cohorts, ok := root["cohorts"].(map[string]any)
	if !ok {
		return nil
	}
	if err := checkCohortCounts(root, cohorts); err != nil {
		return err
	}
	for name, value := range cohorts {
		cohort := value.(map[string]any)
		if _, isInt := integerLiteral(cohort["rejection_count"]); !isInt {
			return fmt.Errorf("previous %s cohort has invalid rejection_count", name)
		}
		if name != "old_product" && name != "development" {
			continue
		}
		if err := checkCohortRecords(name, cohort); err != nil {
			return err
		}
	}
	product, _ := cohorts["old_product"].(map[string]any)
	if !jsonEqual(root["non_regression"], product["non_regression"], false) {
		return fmt.Errorf("previous top-level non_regression differs from the old_product records")
	}
	return checkFloorsAgainstMetrics(root, cohorts)
}

func checkCohortCounts(root, cohorts map[string]any) error {
	rows, isList := root["cases"].([]any)
	if !isList {
		return fmt.Errorf("previous report has no case records")
	}
	ids := map[string]map[string]bool{}
	for name, value := range cohorts {
		if _, isObject := value.(map[string]any); !isObject {
			return fmt.Errorf("previous %s cohort is not an object", name)
		}
		ids[name] = map[string]bool{}
	}
	for _, item := range rows {
		row, _ := item.(map[string]any)
		id, _ := row["case_id"].(string)
		cohort, _ := row["cohort"].(string)
		if id == "" || ids[cohort] == nil {
			return fmt.Errorf("previous report has an invalid case identity or cohort")
		}
		ids[cohort][id] = true
		if comparedCases[cohort] {
			if err := checkCaseRow(row); err != nil {
				return err
			}
		}
	}
	if ids["old_product"] != nil {
		rejections, isList := root["rejections"].([]any)
		if !isList {
			return fmt.Errorf("previous report has no rejection records")
		}
		for _, item := range rejections {
			row, _ := item.(map[string]any)
			if id, _ := row["case_id"].(string); id != "" {
				ids["old_product"][id] = true
			}
		}
	}
	for name, set := range ids {
		count, isInt := integerLiteral(cohorts[name].(map[string]any)["case_count"])
		if !isInt || count != int64(len(set)) {
			return fmt.Errorf("previous %s case_count differs from case records", name)
		}
	}
	return nil
}

func checkCaseRow(row map[string]any) error {
	for _, key := range []string{"mode", "arms"} {
		if _, isString := row[key].(string); !isString {
			return fmt.Errorf("previous case record has invalid %s", key)
		}
	}
	if _, isInt := integerLiteral(row["limit"]); !isInt {
		return fmt.Errorf("previous case record has invalid limit")
	}
	expected, isInt := integerLiteral(row["expected"])
	ranks, isList := row["ranks"].([]any)
	if !isInt || !isList || int64(len(ranks)) != expected {
		return fmt.Errorf("previous case record has invalid expected or ranks")
	}
	for _, rank := range ranks {
		if n, ok := integerLiteral(rank); !ok || n < 0 {
			return fmt.Errorf("previous case record has an invalid rank")
		}
	}
	return nil
}

func checkCohortRecords(name string, cohort map[string]any) error {
	populated := false
	if count, ok := integerLiteral(cohort["case_count"]); ok && count > 0 {
		populated = true
	}
	if !populated {
		return nil
	}
	for _, key := range []string{"metrics", "non_regression"} {
		if err := checkRecordList(name+" "+key, cohort[key]); err != nil {
			return err
		}
	}
	return nil
}

func checkRecordList(label string, value any) error {
	list, isList := value.([]any)
	if !isList {
		return fmt.Errorf("previous %s is not a list", label)
	}
	type recordKey struct {
		mode, arms, name string
		limit            int64
	}
	seen := map[recordKey]bool{}
	for _, item := range list {
		record, isObject := item.(map[string]any)
		if !isObject {
			return fmt.Errorf("previous %s record is not an object", label)
		}
		for _, key := range []string{"mode", "arms", "name"} {
			if _, isString := record[key].(string); !isString {
				return fmt.Errorf("previous %s record has invalid %s", label, key)
			}
		}
		limit, isInt := integerLiteral(record["limit"])
		if !isInt {
			return fmt.Errorf("previous %s record has invalid limit", label)
		}
		mode, _ := record["mode"].(string)
		arms, _ := record["arms"].(string)
		name, _ := record["name"].(string)
		identity := recordKey{mode, arms, name, limit}
		if seen[identity] {
			return fmt.Errorf("previous %s has a duplicate record %s/%s/%d/%s", label, mode, arms, limit, name)
		}
		seen[identity] = true
	}
	return nil
}

func checkFloorsAgainstMetrics(root, cohorts map[string]any) error {
	product, _ := cohorts["old_product"].(map[string]any)
	if top, present := root["floors"]; present || product["floors"] != nil {
		if !jsonEqual(top, product["floors"], false) {
			return fmt.Errorf("previous top-level floors differ from the old_product floors")
		}
	}
	for _, name := range []string{"old_product", "development"} {
		cohort, _ := cohorts[name].(map[string]any)
		floors, _ := cohort["floors"].([]any)
		metrics, _ := cohort["metrics"].([]any)
		for _, item := range floors {
			floor, _ := item.(map[string]any)
			if floor["status"] != "pass" {
				continue
			}
			metric := map[string]any{}
			for key, value := range floor {
				if key != "floor" && key != "status" {
					metric[key] = value
				}
			}
			matched := false
			for _, candidate := range metrics {
				if jsonEqual(candidate, any(metric), false) {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("previous %s floor record differs from its cohort metric", name)
			}
		}
	}
	return nil
}
