package evaluation

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/retrieval"
)

type restoredCaseEngine struct {
	fakeEngine
	legacyCalls int
}

func (e *restoredCaseEngine) Prepare(_ context.Context, req retrieval.Request) (retrieval.Prepared, error) {
	return retrieval.Prepared{Request: req, Validation: retrieval.Validation{Accepted: true}, EffectiveQuery: "ngân hàng", QueryChanged: true, Vector: &embedding.Vector{Values: []float32{1}}}, nil
}
func (e *restoredCaseEngine) PrepareLegacy(_ context.Context, req retrieval.Request) (retrieval.Prepared, error) {
	e.legacyCalls++
	return retrieval.Prepared{Request: req, Validation: retrieval.Validation{Accepted: true}, EffectiveQuery: req.Query, Vector: &embedding.Vector{Values: []float32{2}}}, nil
}
func TestPrefetchLegacyUsesTypedTextAfterRestoration(t *testing.T) {
	e := &restoredCaseEngine{}
	input := cohortInput{c: Case{Question: "ngan hang"}, cohorts: []string{"legacy", "old_product"}, legacy: true}
	got := prefetchCase(context.Background(), e, input, nil, nil, retrieval.Recipe4(), ValidityOverrides{})
	if e.legacyCalls != 1 || got.legacy.EffectiveQuery != "ngan hang" || got.legacy.Vector.Values[0] != 2 {
		t.Fatalf("legacy calls %d, result %+v", e.legacyCalls, got.legacy)
	}
}
