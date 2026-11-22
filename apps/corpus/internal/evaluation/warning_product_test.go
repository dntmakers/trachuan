package evaluation

import (
	"context"
	"errors"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type warningProductEngine struct {
	recordedEngine
	warning         retrieval.Warning
	calls           []retrieval.ReadRequest
	warningRequests []retrieval.Request
	readError       bool
}

func (e *warningProductEngine) Collect(ctx context.Context, prepared retrieval.Prepared, depths retrieval.Depths) (retrieval.Arms, error) {
	if prepared.Request.Query == "Quy định gì?" {
		e.warningRequests = append(e.warningRequests, prepared.Request)
	}
	return e.recordedEngine.Collect(ctx, prepared, depths)
}

func (e *warningProductEngine) Present(context.Context, retrieval.Response, retrieval.Detail) (retrieval.SearchResult, error) {
	return retrieval.SearchResult{NonCurrent: []retrieval.PresentedHit{{Handle: "vn:vbpl:1#dieu-5", Warnings: []retrieval.Warning{e.warning}}}}, nil
}
func (e *warningProductEngine) Read(_ context.Context, req retrieval.ReadRequest) (retrieval.ReadResult, error) {
	if e.readError {
		return retrieval.ReadResult{}, errors.New("read unavailable")
	}
	e.calls = append(e.calls, req)
	return retrieval.ReadResult{Release: req.Release, Items: []retrieval.ItemResult{{Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "dieu-5", Warnings: []retrieval.Warning{e.warning}}}}}}, nil
}

func TestRun_WarningProductSurfaces(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	c := WarningCase{EntryID: "effect-example", Question: "Quy định gì?", Target: "vn:vbpl:1#dieu-5", Acting: "vn:vbpl:2#dieu-30", RelationKind: "repeals"}
	suite := recordedSuite()
	suite.WarningCases = []WarningCase{c}
	w := retrieval.Warning{Pointer: retrieval.Pointer{Dir: "in", Kind: c.RelationKind, Key: "vn:vbpl:2", InRelease: true}, Source: c.Acting, Target: c.Target, Exact: true}
	engine := &warningProductEngine{recordedEngine: recordedEngine{fakeEngine: fakeEngine{date: date}}, warning: w}
	opts := Options{EvaluationDate: date, WarningResults: []WarningResult{{EntryID: c.EntryID, Outcome: "applied", Search: []WarningSearch{}}}, WarningScope: func(context.Context, string, string) (bool, error) { return true, nil }}
	r, _, err := Run(context.Background(), engine, suite, opts)
	if err != nil || !r.QualityPass || len(r.WarningCases) != 1 || len(r.WarningCases[0].Search) != 8 || r.WarningCases[0].Read == nil || len(engine.calls) != 1 {
		t.Fatalf("report = %+v, error %v", r.WarningCases, err)
	}
	if len(engine.warningRequests) != 2 {
		t.Fatalf("warning request count = %d", len(engine.warningRequests))
	}
	for i, req := range engine.warningRequests {
		if req.InForceOnly == nil || *req.InForceOnly || req.AsOf != nil || req.Arms != "hybrid" || req.Mode != []retrieval.Mode{"exact", "ann"}[i] {
			t.Fatalf("warning request = %+v", req)
		}
	}
	opts.WarningResults[0].Outcome = "redundant"
	redundant, _, err := Run(context.Background(), engine, suite, opts)
	if err != nil || !redundant.QualityPass || redundant.WarningCases[0].Read == nil {
		t.Fatalf("redundant report = %+v, error %v", redundant.WarningCases, err)
	}
	engine.warning.Exact = false
	r, _, err = Run(context.Background(), engine, suite, opts)
	if err != nil || r.QualityPass || r.WarningCases[0].Read != nil {
		t.Fatalf("inexact report = %+v, error %v", r.WarningCases, err)
	}
	engine.readError = true
	if _, _, err = Run(context.Background(), engine, suite, opts); err == nil {
		t.Fatal("read execution error did not abort")
	}
}
