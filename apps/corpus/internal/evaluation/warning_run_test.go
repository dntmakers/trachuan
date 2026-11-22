package evaluation

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestRun_BoundMissingWarningFailsQuality(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	suite := recordedSuite()
	suite.WarningCases = []WarningCase{{EntryID: "effect-example"}}
	options := Options{EvaluationDate: date, WarningResults: []WarningResult{{EntryID: "effect-example", Outcome: "missing", Search: []WarningSearch{}}}}
	r, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, options)
	if err != nil {
		t.Fatal(err)
	}
	if r.QualityPass || len(r.WarningCases) != 1 || r.WarningCases[0].Outcome != "missing" {
		t.Fatalf("warning report = %+v", r.WarningCases)
	}
}
