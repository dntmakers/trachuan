package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type recipe4Engine struct{ recordedEngine }

func (e *recipe4Engine) Release() retrieval.Release {
	r := e.recordedEngine.Release()
	r.RetrievalRecipe = retrieval.Recipe4()
	threshold := 0.0
	r.RetrievalRecipe.Abstention.Threshold = &threshold
	return r
}

func TestParseDecisionReport_Recipe4Version5(t *testing.T) {
	recipe := retrieval.Recipe4()
	raw, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	data := decisionReport(t, func(r map[string]any) {
		r["version"] = 5
		r["scorer"] = "agent-eval/6"
		r["retrieval_recipe"] = json.RawMessage(raw)
		r["recipes"] = map[string]string{"retrieval": recipe.Hash()}
	})
	if _, err := ParseDecisionReport(data); err != nil {
		t.Fatal(err)
	}
}

func TestParseDecisionReport_Recipe4RequiresScorer6(t *testing.T) {
	recipe := retrieval.Recipe4()
	raw, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	data := decisionReport(t, func(r map[string]any) {
		r["version"] = 5
		r["scorer"] = "agent-eval/5"
		r["retrieval_recipe"] = json.RawMessage(raw)
		r["recipes"] = map[string]string{"retrieval": recipe.Hash()}
	})
	if _, err := ParseDecisionReport(data); err == nil {
		t.Fatal("accepted wrong scorer")
	}
}

func TestReportTransition_Recipe4Cutover(t *testing.T) {
	previous := toleranceReport(t, .8)
	previous.Version = 4
	for i := range previous.Rejections {
		previous.Rejections[i].Reason = "diacritics_required"
	}
	current := toleranceReport(t, .8)
	current.Version = 5
	current.Rejections = []Rejection{}
	cohort := current.Cohorts["old_product"]
	cohort.CaseCount -= cohort.RejectionCount
	cohort.RejectionCount = 0
	current.Cohorts["old_product"] = cohort
	if err := validateReportTransition(&current, &previous); err != nil {
		t.Fatalf("valid cutover: %v", err)
	}
	if !sameMRRCohort(&current, &previous, "old_product") {
		t.Fatal("scored membership differs")
	}
}

func cutoverPair(t *testing.T) (current, previous Report) {
	t.Helper()
	previous = toleranceReport(t, .8)
	previous.Version = 4
	current = toleranceReport(t, .8)
	current.Version = 5
	current.Rejections = []Rejection{}
	cohort := current.Cohorts["old_product"]
	cohort.CaseCount -= cohort.RejectionCount
	cohort.RejectionCount = 0
	current.Cohorts["old_product"] = cohort
	return current, previous
}

func TestReportTransition_CutoverRefusesDifferentScoredMembership(t *testing.T) {
	current, previous := cutoverPair(t)
	if err := validateReportTransition(&current, &previous); err != nil {
		t.Fatalf("valid cutover: %v", err)
	}
	for i := range previous.Cases {
		if previous.Cases[i].Cohort == "old_product" {
			previous.Cases[i].Expected++
		}
	}
	if err := validateReportTransition(&current, &previous); err == nil {
		t.Fatal("accepted different old product scored membership")
	}
}

func TestSameMRRCohort_CutoverIgnoresPrecisionDenominatorOnly(t *testing.T) {
	setDenominator := func(r *Report, name string) {
		c := r.Cohorts["old_product"]
		for i := range c.Metrics {
			if c.Metrics[i].Name == name {
				c.Metrics[i].Denominator = 7
			}
		}
		r.Cohorts["old_product"] = c
	}
	current, previous := cutoverPair(t)
	setDenominator(&previous, "current_law_precision")
	if !sameMRRCohort(&current, &previous, "old_product") {
		t.Fatal("precision denominator blocked the cutover")
	}
	for _, name := range []string{"recall", "mrr"} {
		current, previous = cutoverPair(t)
		setDenominator(&previous, name)
		if sameMRRCohort(&current, &previous, "old_product") {
			t.Fatalf("%s denominator difference accepted", name)
		}
	}
	current, previous = cutoverPair(t)
	current.Version = 4
	setDenominator(&previous, "current_law_precision")
	if sameMRRCohort(&current, &previous, "old_product") {
		t.Fatal("precision denominator ignored outside the cutover")
	}
}

func TestRun_Recipe4ComparedWithVersion4(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	old, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	e := &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}
	current, _, err := Run(context.Background(), e, suite, Options{EvaluationDate: date, PreviousReport: path})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 5 || current.PreviousDiff == nil {
		t.Fatal("missing version 5 comparison")
	}
	diff, ok := current.PreviousDiff.(map[string]any)
	if !ok {
		t.Fatalf("diff type %T", current.PreviousDiff)
	}
	if _, ok = diff["previous"]; !ok {
		t.Fatalf("no previous in diff: %v", diff)
	}
	if reason, ok := diff["reason"]; ok {
		t.Fatalf("comparison declined: %v", reason)
	}
}

func TestCutoverPrevious_RejectsInvalidPrecision(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	r, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Cohorts["old_product"]
	for i := range c.Metrics {
		if c.Metrics[i].Name == "current_law_precision" && c.Metrics[i].Mode == "exact" && c.Metrics[i].Arms == "hybrid" {
			c.Metrics[i].Denominator++
		}
	}
	r.Cohorts["old_product"] = c
	if err := validateCutoverPrevious(&r, suite); err == nil {
		t.Fatal("accepted inconsistent previous precision")
	}
}

func TestCutoverPrevious_AcceptsUnavailablePrecision(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	r, _, err := Run(context.Background(), &fakeEngine{date: date}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCutoverPrevious(&r, suite); err != nil {
		t.Fatalf("valid unavailable precision rejected: %v", err)
	}
}

func TestCutoverPrevious_AcceptsFailedPrecision(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	r, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	cohort := r.Cohorts["old_product"]
	for i := range cohort.Metrics {
		m := &cohort.Metrics[i]
		if m.Name == "current_law_precision" && m.Arms == "hybrid" {
			m.Numerator, m.Value = 0, &zero
		}
	}
	for i := range r.Floors {
		r.Floors[i].Numerator, r.Floors[i].Value, r.Floors[i].Status = 0, &zero, "fail"
	}
	cohort.Floors = r.Floors
	r.Cohorts["old_product"] = cohort
	if err := validateCutoverPrevious(&r, suite); err != nil {
		t.Fatalf("valid failed precision rejected: %v", err)
	}
}

func TestCutoverPrevious_RejectsInvalidFloor(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	r, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	r.Floors[0].Status = "fail"
	if err := validateCutoverPrevious(&r, suite); err == nil {
		t.Fatal("accepted invalid previous floor")
	}
}

func TestCutoverPrevious_RejectsWrongRejection(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	suite := recordedSuite()
	r, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	r.Rejections[0].CaseID = "other"
	if err := validateCutoverPrevious(&r, suite); err == nil {
		t.Fatal("accepted rejection outside fixture")
	}
}

func TestRun_Recipe4Report(t *testing.T) {
	date := retrieval.Date{Year: 2026, Month: time.November, Day: 9}
	e := &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}
	r, _, err := Run(context.Background(), e, testSuite(t), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 5 || r.Scorer != "agent-eval/6" || r.Cohorts["old_product"].CaseCount != 89 {
		t.Fatalf("version %d scorer %s old product cases %d", r.Version, r.Scorer, r.Cohorts["old_product"].CaseCount)
	}
	for name, cohort := range r.Cohorts {
		if cohort.RejectionCount != 0 {
			t.Fatalf("%s rejections: %d", name, cohort.RejectionCount)
		}
	}
	b, err := json.Marshal(r.Rejections)
	if err != nil || string(b) != "[]" {
		t.Fatalf("rejections: %s, %v", b, err)
	}
}
