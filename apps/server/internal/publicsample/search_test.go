package publicsample

import (
	"context"
	"errors"
	"testing"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type sampleEngine struct {
	t         *testing.T
	instant   time.Time
	raw       retrieval.Response
	presented retrieval.SearchResult
	err       error
}

func (e sampleEngine) Search(_ context.Context, req retrieval.Request) (retrieval.Response, error) {
	if req.Query != Query || req.Limit != 1 || req.InForceOnly == nil || !*req.InForceOnly || !req.Instant.Equal(e.instant) || req.AsOf != nil {
		e.t.Fatalf("unexpected request %+v", req)
	}
	return e.raw, e.err
}
func (e sampleEngine) Present(_ context.Context, _ retrieval.Response, detail retrieval.Detail) (retrieval.SearchResult, error) {
	if detail != retrieval.DetailCompact {
		e.t.Fatalf("detail %s", detail)
	}
	return e.presented, e.err
}

func TestSearchSample_ProjectsCompactSearch(t *testing.T) {
	raw, presented := sample()
	instant := time.Date(2099, 1, 1, 7, 0, 0, 0, time.UTC)
	body, err := searchSample(context.Background(), sampleEngine{t: t, instant: instant, raw: raw, presented: presented}, "20990101", instant)
	if err != nil {
		t.Fatal(err)
	}
	validateSampleSchema(t, body)
}

func TestSearchSample_PropagatesSearchFailure(t *testing.T) {
	instant := time.Now()
	if _, err := searchSample(context.Background(), sampleEngine{t: t, instant: instant, err: errors.New("failure")}, "20990101", instant); err == nil {
		t.Fatal("accepted failure")
	}
}
