package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

var (
	floorRequired = []string{"mode", "arms", "limit", "name", "numerator", "denominator", "value"}
	floorOptional = []string{"non_current_tail", "not_yet_eligible", "cap_override", "article_cap_override", "historical_policy", "mapping_incomplete_count"}
)

func validatePreviousFloorFields(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var root map[string]any
	if err := d.Decode(&root); err != nil {
		return fmt.Errorf("decode previous floors: %w", err)
	}
	if err := validatePreviousShape(root); err != nil {
		return err
	}
	cohorts, _ := root["cohorts"].(map[string]any)
	for _, name := range []string{"old_product", "development"} {
		cohort, ok := cohorts[name].(map[string]any)
		if !ok {
			continue
		}
		if err := checkFloorList(cohort["floors"]); err != nil {
			return err
		}
	}
	return nil
}

func checkFloorList(value any) error {
	list, _ := value.([]any)
	for _, item := range list {
		record, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("previous floor record is not an object")
		}
		if err := checkFloorRecord(record, true); err != nil {
			return err
		}
	}
	return nil
}

func checkFloorRecord(record map[string]any, floor bool) error {
	allowed := map[string]bool{}
	for _, key := range append(append([]string{}, floorRequired...), floorOptional...) {
		allowed[key] = true
	}
	required := append([]string{}, floorRequired...)
	if floor {
		allowed["floor"], allowed["status"] = true, true
		required = append(required, "floor", "status")
	}
	for _, key := range required {
		if _, ok := record[key]; !ok {
			return fmt.Errorf("previous floor record lacks %s", key)
		}
	}
	for key := range record {
		if !allowed[key] {
			return fmt.Errorf("previous floor record has unexpected field %s", key)
		}
	}
	_, limitInt := integerLiteral(record["limit"])
	_, denominatorInt := integerLiteral(record["denominator"])
	if !limitInt || !denominatorInt || !finiteNumber(record["numerator"]) || (record["value"] != nil && !finiteNumber(record["value"])) {
		return fmt.Errorf("previous floor record has invalid number types")
	}
	for _, key := range []string{"name", "arms", "mode"} {
		if _, ok := record[key].(string); !ok {
			return fmt.Errorf("previous floor record has invalid %s", key)
		}
	}
	if marker, present := record["not_yet_eligible"]; present {
		if _, isBool := marker.(bool); !isBool {
			return fmt.Errorf("previous floor record has invalid not_yet_eligible")
		}
	}
	if count, present := record["mapping_incomplete_count"]; present {
		if _, ok := integerLiteral(count); !ok {
			return fmt.Errorf("previous floor record has invalid mapping_incomplete_count")
		}
	}
	return nil
}

func integerLiteral(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(string(number), 10, 64)
	return n, err == nil
}

func finiteNumber(value any) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	f, err := strconv.ParseFloat(string(number), 64)
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}
