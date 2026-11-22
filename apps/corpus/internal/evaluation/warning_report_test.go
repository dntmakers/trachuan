package evaluation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestWarningReport_EmptyAndFailed(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	r, _, err := Run(context.Background(), &fakeEngine{date: date}, recordedSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"warning_cases":[]`) || r.NotRun["warning_cases"] != "not_run" {
		t.Fatalf("empty warning report: %s", data)
	}
	failed := Report{QualityPass: true, NotRun: map[string]string{}}
	finishWarnings(&failed, []WarningResult{{EntryID: "effect-example", Outcome: "missing", Search: []WarningSearch{}}}, []retrieval.Mode{"exact", "ann"})
	if failed.QualityPass {
		t.Fatal("missing warning passed")
	}
	data, err = json.Marshal(failed.WarningCases[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"entry_id":"effect-example","outcome":"missing","search":[],"read":null}` {
		t.Fatalf("record = %s", data)
	}
	if _, ok := failed.NotRun["warning_cases"]; ok {
		t.Fatal("nonempty fixture marked not_run")
	}
}

func TestWarningReport_RequiresEverySurface(t *testing.T) {
	match := &WarningMatch{Carrier: "vn:1#dieu-1", Dir: "in", Kind: "repeals", Key: "vn:2", InRelease: true, Source: "vn:2#dieu-2", Target: "vn:1#dieu-1", Exact: true}
	row := WarningResult{EntryID: "effect-one", Outcome: "applied", Read: match}
	for _, mode := range []retrieval.Mode{"exact", "ann"} {
		for _, limit := range []int{8, 20} {
			for _, detail := range []retrieval.Detail{retrieval.DetailCompact, retrieval.DetailFull} {
				row.Search = append(row.Search, WarningSearch{Mode: mode, Limit: limit, Detail: detail, Match: match})
			}
		}
	}
	for _, outcome := range []string{"applied", "redundant", "stale", "missing"} {
		r := Report{QualityPass: true, NotRun: map[string]string{}}
		row.Outcome = outcome
		finishWarnings(&r, []WarningResult{row}, []retrieval.Mode{"exact", "ann"})
		if r.QualityPass != (outcome == "applied" || outcome == "redundant") {
			t.Fatalf("%s: quality %v", outcome, r.QualityPass)
		}
	}
	row.Outcome = "applied"
	for i := range row.Search {
		original := row.Search[i].Match
		row.Search[i].Match = nil
		r := Report{QualityPass: true, NotRun: map[string]string{}}
		finishWarnings(&r, []WarningResult{row}, []retrieval.Mode{"exact", "ann"})
		if r.QualityPass {
			t.Fatalf("missing search surface %d passed", i)
		}
		row.Search[i].Match = original
	}
	row.Read = nil
	r := Report{QualityPass: true, NotRun: map[string]string{}}
	finishWarnings(&r, []WarningResult{row}, []retrieval.Mode{"exact", "ann"})
	if r.QualityPass {
		t.Fatal("missing read passed")
	}
}
