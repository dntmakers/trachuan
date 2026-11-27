package evaluation

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type ruleTamper struct {
	name   string
	want   string
	mutate func(root map[string]any)
}

func allFloorCopies(root map[string]any) []map[string]any {
	copies := []map[string]any{root["floors"].([]any)[0].(map[string]any)}
	copies = append(copies, cohortObject(root, "old_product")["floors"].([]any)[0].(map[string]any))
	return copies
}

func setFloorAndMetric(root map[string]any, key string, value any) {
	for _, record := range allFloorCopies(root) {
		record[key] = value
	}
	for _, metric := range precisionMetrics(root, "old_product") {
		if metric["mode"] == "exact" {
			metric[key] = value
		}
	}
}

func previousRuleTampers() []ruleTamper {
	return []ruleTamper{
		{"old product case_count one too high", "old_product case_count differs from case records", func(r map[string]any) {
			cohort := cohortObject(r, "old_product")
			n, _ := integerLiteral(cohort["case_count"])
			cohort["case_count"] = json.Number(string(rune('0' + n + 1)))
		}},
		{"old product case_count one too low", "old_product case_count differs from case records", func(r map[string]any) {
			cohort := cohortObject(r, "old_product")
			n, _ := integerLiteral(cohort["case_count"])
			cohort["case_count"] = json.Number(string(rune('0' + n - 1)))
		}},
		{"case row without mode", "previous case record has invalid mode", func(r map[string]any) { delete(firstRow(r, "old_product"), "mode") }},
		{"case row null arms", "previous case record has invalid arms", func(r map[string]any) { firstRow(r, "old_product")["arms"] = nil }},
		{"case row null limit", "previous case record has invalid limit", func(r map[string]any) { firstRow(r, "old_product")["limit"] = nil }},
		{"case row without expected", "invalid expected or ranks", func(r map[string]any) { delete(firstRow(r, "old_product"), "expected") }},
		{"case row null ranks", "invalid expected or ranks", func(r map[string]any) { firstRow(r, "old_product")["ranks"] = nil }},
		{"case row ranks shorter than expected", "invalid expected or ranks", func(r map[string]any) {
			row := firstRow(r, "old_product")
			row["expected"] = json.Number("2")
		}},
		{"case row null rank", "invalid rank", func(r map[string]any) { firstRow(r, "old_product")["ranks"] = []any{nil} }},
		{"case row negative rank", "invalid rank", func(r map[string]any) { firstRow(r, "old_product")["ranks"] = []any{json.Number("-1")} }},
		{"development duplicate metric", "development metrics has a duplicate record", func(r map[string]any) {
			metrics := cohortObject(r, "development")["metrics"].([]any)
			metrics[1] = metrics[0]
		}},
		{"development duplicate comparison", "development non_regression has a duplicate record", func(r map[string]any) {
			records := cohortObject(r, "development")["non_regression"].([]any)
			records[1] = records[0]
		}},
		{"floor differs from its metric", "floor record differs from its cohort metric", func(r map[string]any) {
			for _, record := range allFloorCopies(r) {
				record["numerator"] = json.Number("7")
			}
		}},
		{"mapping count null", "invalid mapping_incomplete_count", func(r map[string]any) { setFloorAndMetric(r, "mapping_incomplete_count", nil) }},
		{"not_yet_eligible null", "invalid not_yet_eligible", func(r map[string]any) { setFloorAndMetric(r, "not_yet_eligible", nil) }},
	}
}

func TestPreviousRules_EachRuleNamesItsRefusal(t *testing.T) {
	baseline := floorBaselines(t)["populated"]
	encoded, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	control, err := decodePreviousReport(encoded)
	if err != nil || control.Version == 0 {
		t.Fatalf("control refused: %v", err)
	}
	for _, tc := range previousRuleTampers() {
		t.Run(tc.name, func(t *testing.T) {
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			var root map[string]any
			if err := decoder.Decode(&root); err != nil {
				t.Fatal(err)
			}
			tc.mutate(root)
			data, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPreviousRules_DevelopmentCaseCountMustMatchRows(t *testing.T) {
	encoded, err := json.Marshal(floorBaselines(t)["populated"])
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var root map[string]any
	if err = decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	cohort := cohortObject(root, "development")
	n, _ := integerLiteral(cohort["case_count"])
	cohort["case_count"] = json.Number(string(rune('0' + n + 1)))
	data, _ := json.Marshal(root)
	if _, err = decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), "development case_count differs from case records") {
		t.Fatalf("error = %v", err)
	}
}

func recipeWithFloatLiteral(t *testing.T) (retrieval.Recipe, json.RawMessage) {
	t.Helper()
	recipe := retrieval.Recipe3()
	threshold := 0.0
	recipe.Abstention.Threshold = &threshold
	known, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(known), `"threshold":0,`) {
		t.Fatalf("Go no longer writes an integral float as an int literal: %s", known)
	}
	return recipe, json.RawMessage(strings.Replace(string(known), `"threshold":0,`, `"threshold":0.0,`, 1))
}

func TestPreviousRecipe_FloatTypedFieldMustKeepItsLiteralKind(t *testing.T) {
	recipe, written := recipeWithFloatLiteral(t)
	report := decisionReport(t, func(r map[string]any) {
		r["retrieval_recipe"] = written
		r["recipes"] = map[string]string{"retrieval": recipe.Hash()}
	})
	if _, err := ParseDecisionReport(report); err == nil || !strings.Contains(err.Error(), "unknown or missing fields") {
		t.Fatalf("report recipe: error = %v", err)
	}
	if err := validatePreviousRecipeFields(written, recipe); err == nil || !strings.Contains(err.Error(), "unknown or missing fields") {
		t.Fatalf("validatePreviousRecipeFields: error = %v", err)
	}
	asInt, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err = validatePreviousRecipeFields(asInt, recipe); err != nil {
		t.Fatalf("control refused: %v", err)
	}
}

func TestDecodePreviousReport_RecipeObjectIsScannedByTheMRRCheck(t *testing.T) {
	report := decisionReport(t, func(r map[string]any) {
		r["retrieval_recipe"].(map[string]any)["probe"] = map[string]any{"name": "mrr", "gating": true}
	})
	if _, err := decodePreviousReport(report); err == nil || !strings.Contains(err.Error(), "previous MRR comparison missing") {
		t.Fatalf("error = %v", err)
	}
}
