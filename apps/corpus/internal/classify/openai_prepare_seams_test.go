package classify

import (
	"context"
	"strings"
	"testing"
)

func TestArticlePlanProviderCount(t *testing.T) {
	r := articleRecipe(t)
	r.InputBound = 150
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	plan, err := buildArticlePlan(context.Background(), r, articleInputs(2, "c"), map[string]Outcome{"vn:vbpl:1": {Outcome: "ok", Codes: []string{"1.1"}}}, articleNodes(), func(_ context.Context, req ArticleRequest) (int64, error) { return int64(len(req.Articles) * 100), nil })
	if err != nil || len(plan.Requests) != 2 {
		t.Fatalf("plan %+v: %v", plan, err)
	}
}

func TestSelectItemsProviderSize(t *testing.T) {
	r, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{Recipe: r, PriceVersion: batchPriceVersion, Items: []Item{{Key: "x", RequestHash: strings.Repeat("a", 64), InputTokens: 100}}}
	called := false
	selected, indexes, summary, err := (&Runner{}).selectItems(s, 1, func(i int) (int, error) { called = i == 0; return 10, nil }, selectionLimits{items: 1, bytes: 10}, nil, retainedStore{})
	if err != nil || !called || len(indexes) != 1 || indexes[0] != 0 || summary.Requests != 1 || selected.ReserveMicroUSD <= 0 {
		t.Fatalf("selection %+v %v %+v %v", selected, indexes, summary, err)
	}
}
