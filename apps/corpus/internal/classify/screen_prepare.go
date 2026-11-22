package classify

import (
	"context"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type screenBatch struct {
	submission Submission
	requests   []batchapi.Request
}

func (r *Runner) prepareScreenBatch(ctx context.Context, o Options, docs []Document, families []Family, history keyHistory) (*screenBatch, error) {
	if o.article != nil {
		return r.prepareArticleBatch(ctx, o, history)
	}
	selected, err := screenDocuments(o, docs)
	if err != nil {
		return nil, err
	}
	s := Submission{KeyVersion: keyVersion, PriceVersion: batchPriceVersion, RunID: o.RunID, Country: o.Country, Release: o.Release, ScreenHash: screenHash(o), Recipe: o.Recipe, Families: families, State: "preparing", CreatedAt: r.Now(), Budget: o.Budget, BudgetMicroUSD: o.BudgetMicroUSD, Results: map[string]Outcome{}}
	requests := make([]batchapi.Request, len(selected))
	counter := r.countWith(history, nil)
	for i, d := range selected {
		p, buildErr := BuildRequest(o.Recipe, d, families)
		if buildErr != nil {
			return nil, buildErr
		}
		requests[i] = batchapi.Request{Params: p}
		key, encodeErr := requestKey(&requests[i])
		if encodeErr != nil {
			return nil, encodeErr
		}
		n, countErr := countTokens(ctx, p, counter)
		if countErr != nil {
			return nil, providerError(countErr)
		}
		s.Items = append(s.Items, Item{Key: d.Key, RequestHash: key, InputTokens: n})
	}
	if o.Selection != nil {
		s.Items[0].Selection = o.Selection
	}
	return &screenBatch{submission: s, requests: requests}, nil
}
