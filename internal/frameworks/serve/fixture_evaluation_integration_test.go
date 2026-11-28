//go:build integration

package serve

import (
	"context"

	"danny.vn/trachuan/internal/frameworks/build"
)

func (d fixtureDatabase) Evaluate(_ context.Context, _ build.EvaluationRequest) (build.EvaluationResult, error) {
	return build.EvaluationResult{RetrievalRecipeStatus: "placeholder"}, nil
}
