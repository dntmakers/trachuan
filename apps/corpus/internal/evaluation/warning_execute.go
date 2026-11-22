package evaluation

import (
	"context"
	"fmt"

	"danny.vn/trachuan/internal/retrieval"
)

func evaluateWarnings(ctx context.Context, engine Engine, cases []WarningCase, bound []WarningResult, modes []retrieval.Mode, o Options) ([]WarningResult, error) {
	results := append([]WarningResult(nil), bound...)
	for i, c := range cases {
		if results[i].Outcome != "applied" && results[i].Outcome != "redundant" {
			continue
		}
		reader, ok := engine.(warningReader)
		if !ok || o.WarningScope == nil {
			return nil, fmt.Errorf("warning read or scope unavailable")
		}
		target, err := o.WarningScope(ctx, c.Target, c.Target)
		if err != nil {
			return nil, fmt.Errorf("resolve warning target: %w", err)
		}
		acting, err := o.WarningScope(ctx, c.Acting, c.Acting)
		if err != nil {
			return nil, fmt.Errorf("resolve warning acting provision: %w", err)
		}
		force := false
		prepared, err := engine.Prepare(ctx, retrieval.Request{Query: c.Question, Limit: 8, Mode: "exact", Arms: "hybrid", InForceOnly: &force})
		if err != nil {
			return nil, fmt.Errorf("prepare warning %s: %w", c.EntryID, err)
		}
		if !prepared.Validation.Accepted || prepared.Vector == nil {
			return nil, fmt.Errorf("warning query %s rejected", c.EntryID)
		}
		results[i].Search = []WarningSearch{}
		recipe := engine.Release().RetrievalRecipe
		var lexical *retrieval.LexicalArms
		for _, mode := range modes {
			prepared.Request.Mode = mode
			depths := retrieval.Depths{Dense: recipe.DenseDepth, Lexical: recipe.LexicalDepth}
			if mode == "exact" {
				depths = retrieval.Depths{Dense: 200, Lexical: 100}
			}
			var source *retrieval.LexicalArms
			if mode == "ann" {
				source = lexical
			}
			arms, collectErr := collectEvaluation(ctx, engine, prepared, depths, false, o.ValidityOverrides, source)
			if collectErr != nil {
				return nil, fmt.Errorf("collect warning %s: %w", c.EntryID, collectErr)
			}
			if arms.Date != o.EvaluationDate || arms.AsOf != nil {
				return nil, fmt.Errorf("warning evaluation date mismatch")
			}
			if mode == "exact" {
				lexical = &retrieval.LexicalArms{Primary: arms.Primary.Lexical, NonCurrent: arms.NonCurrent.Lexical, Depth: arms.Depths.Lexical}
			}
			for _, limit := range []int{8, 20} {
				var response retrieval.Response
				if o.ArticleCapOverride != nil {
					response, err = retrieval.ShapeWithArticleCap(arms, recipe, limit, "hybrid", o.CapOverride, nil, o.ArticleCapOverride)
				} else {
					response, err = retrieval.ShapeWithCap(arms, recipe, limit, "hybrid", o.CapOverride)
				}
				if err != nil {
					return nil, fmt.Errorf("shape warning %s: %w", c.EntryID, err)
				}
				for _, detail := range []retrieval.Detail{retrieval.DetailCompact, retrieval.DetailFull} {
					presented, presentErr := engine.Present(ctx, response, detail)
					if presentErr != nil {
						return nil, fmt.Errorf("present warning %s: %w", c.EntryID, presentErr)
					}
					var match *WarningMatch
					if target && acting {
						match, err = matchWarningSearch(ctx, c, presented, o.WarningScope)
						if err != nil {
							return nil, err
						}
					}
					results[i].Search = append(results[i].Search, WarningSearch{Mode: mode, Limit: limit, Detail: detail, Match: match})
				}
			}
		}
		read, err := readWarning(ctx, reader, engine.Release().Rel, c)
		if err != nil {
			return nil, err
		}
		if target && acting {
			results[i].Read = read
		} else {
			results[i].Read = nil
		}
	}
	return results, nil
}
