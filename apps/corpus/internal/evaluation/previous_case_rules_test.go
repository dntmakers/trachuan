package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func periodBaselineRoot(t *testing.T) (map[string]any, []byte) {
	t.Helper()
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	report, _, err := Run(context.Background(), &periodEngine{recordedEngine: recordedEngine{fakeEngine: fakeEngine{date: date}}}, periodSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodePreviousReport(encoded); err != nil {
		t.Fatalf("control refused: %v", err)
	}
	return nil, encoded
}

func freshRoot(t *testing.T, encoded []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	return root
}

func rowWithRanks(t *testing.T, root map[string]any, cohort string) map[string]any {
	t.Helper()
	return rowWithRankCount(t, root, cohort, true)
}

func rowWithRankCount(t *testing.T, root map[string]any, cohort string, positive bool) map[string]any {
	t.Helper()
	for _, item := range root["cases"].([]any) {
		row := item.(map[string]any)
		if row["cohort"] == cohort && (len(row["ranks"].([]any)) > 0) == positive {
			return row
		}
	}
	t.Fatalf("no row (with ranks: %v) in %s", positive, cohort)
	return nil
}

type caseRowTamper struct {
	name, want string
	mutate     func(row map[string]any)
}

func caseRowTampers() []caseRowTamper {
	const invalidRanks = "previous case record has invalid expected or ranks"
	set := func(key string, value any) func(map[string]any) {
		return func(row map[string]any) { row[key] = value }
	}
	drop := func(key string) func(map[string]any) {
		return func(row map[string]any) { delete(row, key) }
	}
	var tampers []caseRowTamper
	for _, field := range []string{"mode", "arms"} {
		invalid := "previous case record has invalid " + field
		tampers = append(tampers,
			caseRowTamper{field + " missing", invalid, drop(field)},
			caseRowTamper{field + " null", invalid, set(field, nil)},
			caseRowTamper{field + " number", "cannot unmarshal", set(field, json.Number("5"))},
			caseRowTamper{field + " bool", "cannot unmarshal", set(field, true)},
			caseRowTamper{field + " list", "cannot unmarshal", set(field, []any{})},
		)
	}
	tampers = append(tampers,
		caseRowTamper{"limit missing", "previous case record has invalid limit", drop("limit")},
		caseRowTamper{"limit null", "previous case record has invalid limit", set("limit", nil)},
		caseRowTamper{"limit string", "cannot unmarshal", set("limit", "8")},
		caseRowTamper{"limit float", "cannot unmarshal", set("limit", json.Number("8.5"))},
		caseRowTamper{"limit float literal", "cannot unmarshal", set("limit", json.Number("8.0"))},
		caseRowTamper{"expected missing", invalidRanks, drop("expected")},
		caseRowTamper{"expected null", invalidRanks, set("expected", nil)},
		caseRowTamper{"expected string", "cannot unmarshal", set("expected", "1")},
		caseRowTamper{"expected float", "cannot unmarshal", set("expected", json.Number("1.5"))},
		caseRowTamper{"expected negative", invalidRanks, func(row map[string]any) {
			row["expected"], row["ranks"] = json.Number("-1"), []any{}
		}},
		caseRowTamper{"expected one more than ranks", invalidRanks, func(row map[string]any) {
			n, _ := integerLiteral(row["expected"])
			row["expected"] = json.Number(fmt.Sprint(n + 1))
		}},
		caseRowTamper{"expected one fewer than ranks", invalidRanks, func(row map[string]any) {
			n, _ := integerLiteral(row["expected"])
			row["expected"] = json.Number(fmt.Sprint(n - 1))
		}},
		caseRowTamper{"ranks missing", invalidRanks, drop("ranks")},
		caseRowTamper{"ranks null", invalidRanks, set("ranks", nil)},
		caseRowTamper{"ranks string", "cannot unmarshal", set("ranks", "x")},
		caseRowTamper{"ranks object", "cannot unmarshal", set("ranks", map[string]any{})},
		caseRowTamper{"ranks longer than expected", invalidRanks, func(row map[string]any) {
			row["ranks"] = append(row["ranks"].([]any), json.Number("1"))
		}},
		caseRowTamper{"ranks shorter than expected", invalidRanks, func(row map[string]any) { row["ranks"] = []any{} }},
		caseRowTamper{"rank null", "previous case record has an invalid rank", func(row map[string]any) { row["ranks"].([]any)[0] = nil }},
		caseRowTamper{"rank negative", "previous case record has an invalid rank", func(row map[string]any) { row["ranks"].([]any)[0] = json.Number("-1") }},
		caseRowTamper{"rank string", "cannot unmarshal", func(row map[string]any) { row["ranks"].([]any)[0] = "1" }},
		caseRowTamper{"rank float", "cannot unmarshal", func(row map[string]any) { row["ranks"].([]any)[0] = json.Number("1.5") }},
	)
	return tampers
}

func TestPreviousCaseRows_EveryCheckedFieldInEveryComparedCohort(t *testing.T) {
	_, encoded := periodBaselineRoot(t)
	for _, cohort := range []string{"old_product", "development", "development_period"} {
		for _, tc := range caseRowTampers() {
			t.Run(cohort+"/"+tc.name, func(t *testing.T) {
				root := freshRoot(t, encoded)
				tc.mutate(rowWithRanks(t, root, cohort))
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
}

func TestPreviousCaseRows_ZeroExpectedRowsStillNeedTheirFields(t *testing.T) {
	_, encoded := periodBaselineRoot(t)
	const invalidRanks = "previous case record has invalid expected or ranks"
	for name, tc := range map[string]func(row map[string]any){
		"expected missing":                func(row map[string]any) { delete(row, "expected") },
		"expected null":                   func(row map[string]any) { row["expected"] = nil },
		"ranks missing":                   func(row map[string]any) { delete(row, "ranks") },
		"ranks null":                      func(row map[string]any) { row["ranks"] = nil },
		"negative expected with no ranks": func(row map[string]any) { row["expected"] = json.Number("-1") },
	} {
		for _, cohort := range []string{"old_product", "development"} {
			t.Run(cohort+"/"+name, func(t *testing.T) {
				root := freshRoot(t, encoded)
				tc(rowWithRankCount(t, root, cohort, false))
				data, _ := json.Marshal(root)
				if _, err := decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), invalidRanks) {
					t.Fatalf("error = %v, want %q", err, invalidRanks)
				}
			})
		}
	}
}

func TestPreviousCaseCounts_EveryCohortMustMatchItsRecords(t *testing.T) {
	_, encoded := periodBaselineRoot(t)
	control := freshRoot(t, encoded)
	cohorts := control["cohorts"].(map[string]any)
	for _, name := range []string{"legacy", "old_product", "development", "development_period"} {
		if _, ok := cohorts[name]; !ok {
			t.Fatalf("baseline lacks the %s cohort", name)
		}
		for _, delta := range []int64{1, -1} {
			if delta < 0 && name == "legacy" {
				continue
			}
			t.Run(fmt.Sprintf("%s/%+d", name, delta), func(t *testing.T) {
				root := freshRoot(t, encoded)
				cohort := root["cohorts"].(map[string]any)[name].(map[string]any)
				n, _ := integerLiteral(cohort["case_count"])
				cohort["case_count"] = json.Number(fmt.Sprint(n + delta))
				data, _ := json.Marshal(root)
				want := "previous " + name + " case_count differs from case records"
				if _, err := decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want %q", err, want)
				}
			})
		}
	}
	for name, tc := range map[string]struct {
		want   string
		mutate func(root map[string]any)
	}{
		"rejection row removed": {"old_product case_count differs from case records", func(r map[string]any) {
			r["rejections"] = r["rejections"].([]any)[1:]
		}},
		"rejection row without case_id": {"invalid previous rejection fields", func(r map[string]any) {
			delete(r["rejections"].([]any)[0].(map[string]any), "case_id")
		}},
		"rejection row null": {"invalid previous rejection fields", func(r map[string]any) { r["rejections"].([]any)[0] = nil }},
		"rejections null":    {"no rejection records", func(r map[string]any) { r["rejections"] = nil }},
		"row moved between cohorts": {"case_count differs from case records", func(r map[string]any) {
			rowWithRanks(t, r, "development")["cohort"] = "development_period"
		}},
		"row in an unknown cohort": {"invalid case identity or cohort", func(r map[string]any) {
			rowWithRanks(t, r, "development")["cohort"] = "elsewhere"
		}},
		"row without case_id": {"invalid case identity or cohort", func(r map[string]any) {
			delete(rowWithRanks(t, r, "development"), "case_id")
		}},
		"row null":      {"invalid case identity or cohort", func(r map[string]any) { r["cases"].([]any)[0] = nil }},
		"cases missing": {"no case records", func(r map[string]any) { delete(r, "cases") }},
		"cohort null":   {"development_period cohort is not an object", func(r map[string]any) { r["cohorts"].(map[string]any)["development_period"] = nil }},
		"legacy case_count null": {"legacy case_count differs from case records", func(r map[string]any) {
			r["cohorts"].(map[string]any)["legacy"].(map[string]any)["case_count"] = nil
		}},
		"legacy case_count missing": {"legacy case_count differs from case records", func(r map[string]any) {
			delete(r["cohorts"].(map[string]any)["legacy"].(map[string]any), "case_count")
		}},
		"case_count null": {"development case_count differs from case records", func(r map[string]any) {
			r["cohorts"].(map[string]any)["development"].(map[string]any)["case_count"] = nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			root := freshRoot(t, encoded)
			tc.mutate(root)
			data, _ := json.Marshal(root)
			if _, err := decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPreviousCaseCounts_EmptyCohortNeedsAnIntegerCount(t *testing.T) {
	encoded, err := json.Marshal(floorBaselines(t)["empty"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodePreviousReport(encoded); err != nil {
		t.Fatalf("control refused: %v", err)
	}
	for name, mutate := range map[string]func(cohort map[string]any){
		"null":    func(cohort map[string]any) { cohort["case_count"] = nil },
		"missing": func(cohort map[string]any) { delete(cohort, "case_count") },
		"one":     func(cohort map[string]any) { cohort["case_count"] = json.Number("1") },
	} {
		t.Run(name, func(t *testing.T) {
			root := freshRoot(t, encoded)
			mutate(root["cohorts"].(map[string]any)["development"].(map[string]any))
			data, _ := json.Marshal(root)
			want := "previous development case_count differs from case records"
			if _, err := decodePreviousReport(data); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}
