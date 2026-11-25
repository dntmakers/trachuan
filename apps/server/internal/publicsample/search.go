package publicsample

import (
	"context"
	"encoding/json"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type searchEngine interface {
	Search(context.Context, retrieval.Request) (retrieval.Response, error)
	Present(context.Context, retrieval.Response, retrieval.Detail) (retrieval.SearchResult, error)
}

func SearchSample(ctx context.Context, engine *retrieval.Engine, release string, instant time.Time) ([]byte, error) {
	return searchSample(ctx, engine, release, instant)
}

func searchSample(ctx context.Context, engine searchEngine, release string, instant time.Time) ([]byte, error) {
	return searchSampleWithReserve(ctx, engine, release, instant, time.Second)
}

func searchSampleWithReserve(ctx context.Context, engine searchEngine, release string, instant time.Time, reserve time.Duration) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		ctx = retrieval.WithRollbackDeadline(ctx, deadline)
		work, cancel := context.WithDeadline(ctx, deadline.Add(-reserve))
		defer cancel()
		ctx = work
	}
	inForce := true
	raw, err := engine.Search(ctx, retrieval.Request{Query: Query, Limit: 1, InForceOnly: &inForce, Instant: instant})
	if err != nil {
		return nil, err
	}
	presented, err := engine.Present(ctx, raw, retrieval.DetailCompact)
	if err != nil {
		return nil, err
	}
	projected, reason := Project(raw, presented, release)
	if reason != "" {
		return nil, reason
	}
	return json.Marshal(projected)
}
