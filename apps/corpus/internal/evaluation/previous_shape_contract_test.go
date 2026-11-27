package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type shapeTamper struct {
	name   string
	want   string
	mutate func(root map[string]any)
}

func firstRow(root map[string]any, cohort string) map[string]any {
	for _, item := range root["cases"].([]any) {
		row := item.(map[string]any)
		if row["cohort"] == cohort {
			return row
		}
	}
	panic("no row for " + cohort)
}

func cohortObject(root map[string]any, name string) map[string]any {
	return root["cohorts"].(map[string]any)[name].(map[string]any)
}

func previousShapeTampers() []shapeTamper {
	return []shapeTamper{
		{"missing country", "identity is missing", func(r map[string]any) { delete(r, "country") }},
		{"other country", "identity is missing or from another country", func(r map[string]any) { r["country"] = "xx" }},
		{"null rel", "identity is missing", func(r map[string]any) { r["rel"] = nil }},
		{"case row without mode", "invalid mode", func(r map[string]any) { delete(firstRow(r, "old_product"), "mode") }},
		{"case row without limit", "invalid limit", func(r map[string]any) { delete(firstRow(r, "old_product"), "limit") }},
		{"case row without arms", "invalid arms", func(r map[string]any) { delete(firstRow(r, "old_product"), "arms") }},
		{"case row without expected", "invalid expected or ranks", func(r map[string]any) { delete(firstRow(r, "old_product"), "expected") }},
		{"case row null ranks", "invalid expected or ranks", func(r map[string]any) { firstRow(r, "old_product")["ranks"] = nil }},
		{"case row null rank", "invalid rank", func(r map[string]any) { firstRow(r, "old_product")["ranks"] = []any{nil} }},
		{"cohort without rejection_count", "old_product cohort has invalid rejection_count", func(r map[string]any) { delete(cohortObject(r, "old_product"), "rejection_count") }},
		{"legacy null rejection_count", "invalid rejection_count", func(r map[string]any) { cohortObject(r, "legacy")["rejection_count"] = nil }},
		{"cohort without case_count", "old_product case_count differs from case records", func(r map[string]any) { delete(cohortObject(r, "old_product"), "case_count") }},
		{"null period cohort", "development_period cohort is not an object", func(r map[string]any) { r["cohorts"].(map[string]any)["development_period"] = nil }},
		{"old product null metrics", "old_product metrics is not a list", func(r map[string]any) { cohortObject(r, "old_product")["metrics"] = nil }},
		{"old product duplicate metric", "old_product metrics has a duplicate record", func(r map[string]any) {
			metrics := cohortObject(r, "old_product")["metrics"].([]any)
			metrics[1] = metrics[0]
		}},
		{"old product duplicate comparison", "old_product non_regression has a duplicate record", func(r map[string]any) {
			records := cohortObject(r, "old_product")["non_regression"].([]any)
			records[1] = records[0]
		}},
		{"old product metric without name", "old_product metrics record has invalid name", func(r map[string]any) {
			delete(cohortObject(r, "old_product")["metrics"].([]any)[0].(map[string]any), "name")
		}},
		{"old product null comparison", "old_product non_regression record is not an object", func(r map[string]any) {
			cohortObject(r, "old_product")["non_regression"].([]any)[0] = nil
		}},
		{"top comparison without mode", "top-level non_regression differs from the old_product records", func(r map[string]any) { delete(r["non_regression"].([]any)[0].(map[string]any), "mode") }},
		{"top comparison differs from old product", "top-level non_regression differs", func(r map[string]any) { r["non_regression"].([]any)[0].(map[string]any)["value"] = json.Number("0.5") }},
		{"comparison null name", "old_product non_regression record has invalid name", func(r map[string]any) {
			cohortObject(r, "old_product")["non_regression"].([]any)[0].(map[string]any)["name"] = nil
		}},
		{"case row float limit", "cannot unmarshal number 8.0", func(r map[string]any) { firstRow(r, "old_product")["limit"] = json.Number("8.0") }},
		{"malformed rel", "identity is missing", func(r map[string]any) { r["rel"] = "x" }},
		{"missing development hash", "identity is missing", func(r map[string]any) { delete(r["fixture_hashes"].(map[string]any), "Development") }},
		{"short development hash", "identity is missing", func(r map[string]any) { r["fixture_hashes"].(map[string]any)["Development"] = "abc" }},
		{"cap override at top", "incompatible cap override", func(r map[string]any) { r["cap_override"] = json.Number("4") }},
		{"article cap override in a case row", "carries a research override", func(r map[string]any) { firstRow(r, "old_product")["article_cap_override"] = json.Number("2") }},
		{"non current tail in a case row", "carries a research override", func(r map[string]any) { firstRow(r, "old_product")["non_current_tail"] = json.Number("1") }},
		{"not yet eligible in a metric", "carries a research override", func(r map[string]any) {
			cohortObject(r, "old_product")["metrics"].([]any)[0].(map[string]any)["not_yet_eligible"] = true
		}},
		{"recipe float literal", "cannot unmarshal number 50.0", func(r map[string]any) {
			recipe := r["retrieval_recipe"].(map[string]any)
			recipe["dense_depth"] = json.Number(string(recipe["dense_depth"].(json.Number)) + ".0")
		}},
		{"old product floor without cap_override", "top-level floors differ from the old_product floors", func(r map[string]any) {
			delete(cohortObject(r, "old_product")["floors"].([]any)[0].(map[string]any), "cap_override")
		}},
		{"top floor without cap_override", "top-level floors differ from the old_product floors", func(r map[string]any) {
			delete(r["floors"].([]any)[0].(map[string]any), "cap_override")
		}},
		{"both floors without cap_override", "floor record differs from its cohort metric", func(r map[string]any) {
			delete(cohortObject(r, "old_product")["floors"].([]any)[0].(map[string]any), "cap_override")
			delete(r["floors"].([]any)[0].(map[string]any), "cap_override")
		}},
	}
}

func writePreviousShapeTampers(t *testing.T, ctx context.Context, dir string, previous Report, engine Engine, suite Suite, date retrieval.Date) {
	t.Helper()
	encoded, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range previousShapeTampers() {
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		var root map[string]any
		if err = decoder.Decode(&root); err != nil {
			t.Fatal(err)
		}
		tc.mutate(root)
		data, marshalErr := json.Marshal(root)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		path := filepath.Join(dir, fmt.Sprintf("invalid-shape-%d", i), "report.json")
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		report, _, runErr := Run(ctx, engine, suite, Options{EvaluationDate: date, PreviousReport: path})
		diff, _ := report.PreviousDiff.(map[string]any)
		got := fmt.Sprint(diff["reason"])
		if runErr != nil {
			got = runErr.Error()
		}
		if (runErr == nil && diff["previous"] != nil) || !strings.Contains(got, tc.want) {
			t.Fatalf("%s: Go answered %q, want %q", tc.name, got, tc.want)
		}
	}
}

func writePreviousWarningRecords(t *testing.T, ctx context.Context, dir string, previous Report, engine Engine, suite Suite, date retrieval.Date) {
	t.Helper()
	encoded, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(encoded, &root); err != nil {
		t.Fatal(err)
	}
	root["warning_cases"] = []any{map[string]any{"entry_id": "a", "name": "mrr", "gating": true, "historical_policy": "agent", "cap_override": 4, "not_yet_eligible": true, "search": []any{map[string]any{"non_current_tail": 1}}}}
	path := filepath.Join(dir, "valid-warning-records", "report.json")
	writeReleaseFile(t, path, root)
	report, _, runErr := Run(ctx, engine, suite, Options{EvaluationDate: date, PreviousReport: path})
	if diff, ok := report.PreviousDiff.(map[string]any); runErr != nil || !ok || diff["previous"] == nil {
		t.Fatalf("Go refused a previous report whose warning records carry unread fields: %v %v", runErr, report.PreviousDiff)
	}
}
