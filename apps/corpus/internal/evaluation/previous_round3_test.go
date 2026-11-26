package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func addSecondLegacyPositive(t *testing.T, suite *Suite) {
	t.Helper()
	suite.Legacy = append(suite.Legacy, Case{ID: "positive2", Question: "Câu hỏi tốt", ExpectedCitations: []Expectation{{DocNumber: "doc"}}})
	entry := suite.Map.Entries[0]
	entry.CaseID = "positive2"
	suite.Map.Entries = append(suite.Map.Entries, entry)
	legacy, old := releaseLegacyFixtures(t, *suite)
	suite.Hashes.Legacy, suite.Hashes.OldProduct = fixtureHash(legacy), fixtureHash(old)
	suite.Map.LegacySHA256 = suite.Hashes.Legacy
}

func removePreviousHybridRow(r *Report, mode retrieval.Mode, limit int) {
	rows := []CaseResult{}
	for _, row := range r.Cases {
		if row.Cohort == "old_product" && row.CaseID == "positive2" && row.Arms == "hybrid" && row.Mode == mode && row.Limit == limit {
			continue
		}
		rows = append(rows, row)
	}
	r.Cases = rows
	cohort := r.Cohorts["old_product"]
	for i := range cohort.Metrics {
		metric := &cohort.Metrics[i]
		if metric.Mode == mode && metric.Arms == "hybrid" && (metric.Name == "recall" && metric.Limit == 20 && limit == 20 || metric.Name == "mrr" && metric.Limit == 8 && limit == 8) {
			value := 1.0
			metric.Numerator, metric.Denominator, metric.Value = 1, 1, &value
		}
	}
	r.Cohorts["old_product"] = cohort
}

func previousRound3TamperCases() []previousTamper {
	return []previousTamper{
		{"empty old product", func(r *Report) {
			c := r.Cohorts["old_product"]
			c.CaseCount, c.PositiveCount, c.NegativeCount, c.ExpectedCitations = 0, 0, 0, 0
			r.Cohorts["old_product"] = c
			rows := []CaseResult{}
			for _, row := range r.Cases {
				if row.Cohort != "old_product" {
					rows = append(rows, row)
				}
			}
			r.Cases = rows
		}},
		{"scored case only dense", func(r *Report) {
			rows := []CaseResult{}
			for _, row := range r.Cases {
				if row.Cohort == "old_product" && row.CaseID == "positive2" && (row.Arms != "dense" || row.Mode != "exact" || row.Limit != 8) {
					continue
				}
				rows = append(rows, row)
			}
			r.Cases = rows
			cohort := r.Cohorts["old_product"]
			for i := range cohort.Metrics {
				metric := &cohort.Metrics[i]
				if metric.Arms == "hybrid" && (metric.Name == "recall" && metric.Limit == 20 || metric.Name == "mrr" && metric.Limit == 8) {
					value := 1.0
					metric.Numerator, metric.Denominator, metric.Value = 1, 1, &value
				}
			}
			r.Cohorts["old_product"] = cohort
		}},
		{"missing exact hybrid 8", func(r *Report) { removePreviousHybridRow(r, "exact", 8) }},
		{"missing exact hybrid 20", func(r *Report) { removePreviousHybridRow(r, "exact", 20) }},
		{"missing ann hybrid 8", func(r *Report) { removePreviousHybridRow(r, "ann", 8) }},
		{"missing ann hybrid 20", func(r *Report) { removePreviousHybridRow(r, "ann", 20) }},
		{"wrong old product member", func(r *Report) {
			for i := range r.Cases {
				if r.Cases[i].Cohort == "old_product" && r.Cases[i].CaseID == "positive" {
					r.Cases[i].CaseID = "other"
				}
			}
		}},
		{"empty development floor", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0].Status = "pass"
			r.Cohorts["development"] = c
		}},
		{"empty development nonzero floor", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0].Numerator = 1
			r.Cohorts["development"] = c
		}},
		{"empty development nonzero denominator", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0].Denominator = 1
			r.Cohorts["development"] = c
		}},
		{"empty development nonnull value", func(r *Report) {
			c := r.Cohorts["development"]
			value := 0.0
			c.Floors[0].Value = &value
			r.Cohorts["development"] = c
		}},
		{"empty development missing floor", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors = c.Floors[:1]
			r.Cohorts["development"] = c
		}},
		{"empty development duplicate floor", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[1] = c.Floors[0]
			r.Cohorts["development"] = c
		}},
		{"empty development swapped floors", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0], c.Floors[1] = c.Floors[1], c.Floors[0]
			r.Cohorts["development"] = c
		}},
		{"empty development non-current override", func(r *Report) {
			c := r.Cohorts["development"]
			value := 2
			c.Floors[0].NonCurrentTail = &value
			r.Cohorts["development"] = c
		}},
		{"empty development not-yet override", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0].NotYetEligible = true
			r.Cohorts["development"] = c
		}},
		{"empty development document cap override", func(r *Report) {
			c := r.Cohorts["development"]
			value := retrieval.CapOverride(2)
			c.Floors[0].CapOverride = &value
			r.Cohorts["development"] = c
		}},
		{"empty development article cap override", func(r *Report) {
			c := r.Cohorts["development"]
			value := retrieval.ArticleCapOverride(2)
			c.Floors[0].ArticleCapOverride = &value
			r.Cohorts["development"] = c
		}},
		{"empty development confusion", func(r *Report) {
			c := r.Cohorts["development"]
			c.Floors[0].Confusion = &Confusion{}
			r.Cohorts["development"] = c
		}},
		{"empty development precision metric", func(r *Report) {
			c := r.Cohorts["development"]
			c.Metrics = append(c.Metrics, c.Floors[0].Metric)
			r.Cohorts["development"] = c
		}},
	}
}

func TestPreviousOldProduct_RealFixtureMembership(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	suite := testSuite(t)
	engine := &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}
	report, _, err := Run(context.Background(), engine, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if report.Cohorts["old_product"].CaseCount != 89 {
		t.Fatalf("old product case_count = %d", report.Cohorts["old_product"].CaseCount)
	}
	if err = validatePreviousOldProduct(&report, suite); err != nil {
		t.Fatal(err)
	}
	for i := range report.Cases {
		if report.Cases[i].Cohort == "old_product" {
			report.Cases[i].CaseID = "not-in-fixture"
			break
		}
	}
	if err = validatePreviousOldProduct(&report, suite); err == nil {
		t.Fatal("accepted case outside the fixture")
	}
}

func TestReview_PreviousVersionFiveMembershipAndEmptyFloors(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	suite := recordedSuite()
	addSecondLegacyPositive(t, &suite)
	engine := &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}
	baseline, _, err := Run(context.Background(), engine, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range previousRound3TamperCases() {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var previous Report
			if err = json.Unmarshal(data, &previous); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&previous)
			data, err = json.Marshal(previous)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "previous.json")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err = Run(context.Background(), engine, suite, Options{EvaluationDate: date, PreviousReport: path}); err == nil {
				t.Fatal("accepted invalid previous report")
			}
		})
	}
}
