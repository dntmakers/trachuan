package run

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/retrieval"
)

type frozenWarningEngine struct{ date retrieval.Date }

func (e frozenWarningEngine) Taxonomy(context.Context, retrieval.TaxonomyRequest) (retrieval.TaxonomyResult, error) {
	return retrieval.TaxonomyResult{}, nil
}
func (e frozenWarningEngine) AnchorNodes(context.Context, []retrieval.AnchorRef) (map[retrieval.AnchorRef][]string, error) {
	return nil, nil
}

func (e frozenWarningEngine) Release() retrieval.Release {
	r := retrieval.Recipe2()
	threshold := 0.0
	r.Abstention.Threshold = &threshold
	return retrieval.Release{Country: "vn", Rel: "20990102_1", RetrievalRecipe: r}
}
func (e frozenWarningEngine) Prepare(_ context.Context, req retrieval.Request) (retrieval.Prepared, error) {
	v, err := retrieval.ValidateQuery(req.Query)
	if err != nil {
		return retrieval.Prepared{}, err
	}
	p := retrieval.Prepared{Request: req, Validation: v}
	if v.Accepted {
		p.Vector = &embedding.Vector{Values: []float32{1}, TokenCount: 1, RecipeHash: "query"}
	}
	return p, nil
}
func (e frozenWarningEngine) PrepareLegacy(ctx context.Context, req retrieval.Request) (retrieval.Prepared, error) {
	p, err := e.Prepare(ctx, req)
	if err == nil {
		p.Validation.Accepted = true
		p.Vector = &embedding.Vector{Values: []float32{1}, TokenCount: 1, RecipeHash: "query"}
	}
	return p, err
}
func (e frozenWarningEngine) Collect(_ context.Context, p retrieval.Prepared, depths retrieval.Depths) (retrieval.Arms, error) {
	return retrieval.Arms{Validation: p.Validation, Depths: depths, Date: e.date}, nil
}
func (e frozenWarningEngine) Present(context.Context, retrieval.Response, retrieval.Detail) (retrieval.SearchResult, error) {
	return retrieval.SearchResult{}, nil
}

func TestDecide_VersionFourFrozenReportComparesInEval(t *testing.T) {
	ctx := context.Background()
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	engine := frozenWarningEngine{date: date}
	suite, err := evaluation.VNSuite()
	if err != nil {
		t.Fatal(err)
	}
	suite.WarningBytes = []byte(`[{"entry_id":"effect-example","question":"Quy định gì?","target":"vn:vbpl:1#dieu-5","acting":"vn:vbpl:2#dieu-30","relation_kind":"repeals"}]`)
	suite.WarningHash = fmt.Sprintf("%x", sha256.Sum256(suite.WarningBytes))
	cases, err := suite.PinnedWarningCases("vn")
	if err != nil || len(cases) != 1 {
		t.Fatalf("synthetic warning fixture: %v", err)
	}
	suite.WarningBytes = []byte("[]\n")
	suite.WarningHash = fmt.Sprintf("%x", sha256.Sum256(suite.WarningBytes))
	suite.WarningCases = nil
	prior, _, err := evaluation.Run(ctx, engine, suite, evaluation.Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	prior.Country, prior.Rel = "vn", "20990101_1"
	data, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "prior.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := stage.Snapshot{}
	in := decideInput(t, ReportChoice{Path: path}, false, &snapshot)
	if _, err = decide(ctx, in); err != nil {
		t.Fatal(err)
	}
	decision := snapshot.PreviousReport
	if decision == nil {
		t.Fatal("missing frozen decision")
	}
	frozen := filepath.Join(in.Artifacts.Abs(decision.Path), stage.PreviousReportFile)
	current, _, err := evaluation.Run(ctx, engine, suite, evaluation.Options{EvaluationDate: date, PreviousReport: frozen})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := current.ComparedRelease()
	if err != nil || previous == nil || previous.ReportSHA256 != decision.SHA256 {
		t.Fatalf("previous = %+v, %v", previous, err)
	}
	if current.Version != 4 || len(current.NonRegression) == 0 || current.NonRegression[0].Status == "no_previous" {
		t.Fatal("version four non-regression not run")
	}
}
