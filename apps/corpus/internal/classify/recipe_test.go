package classify

import (
	"strings"
	"testing"
)

func TestRecipe_BindsEveryRequestSetting(t *testing.T) {
	r, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	base := r.Hash()
	variants := []Recipe{r, r, r, r, r, r, r, r}
	variants[0].Model = "claude-sonnet-5-5"
	variants[1].Effort = "low"
	variants[2].MaxTokens++
	variants[3].InputBound++
	variants[4].F++
	variants[5].PromptVersion++
	variants[6].SchemaVersion++
	variants[7].TaxonomyHash = strings.Repeat("b", 64)
	for i, v := range variants {
		if v.Hash() == base {
			t.Fatalf("setting %d is unbound", i)
		}
	}
	if _, err = NewRecipe("unsupported", r.TaxonomyHash); err == nil {
		t.Fatal("accepted unsupported model")
	}
}

func TestReserve_BatchPricesByModel(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  int64
	}{
		{"claude-haiku-5-5", 12},
		{"claude-sonnet-5-5", 239},
		{"claude-opus-5-5", 478},
		{"claude-fable-5-2", 955},
	} {
		t.Run(tc.model, func(t *testing.T) {
			r, err := NewRecipe(tc.model, strings.Repeat("a", 64))
			if err != nil {
				t.Fatal(err)
			}
			r.MaxTokens = 17
			if got, err := r.Reserve(123); err != nil || got != tc.want {
				t.Fatalf("reserve = %d micro-USD, error %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestReserve_RoundsUpAndRejectsTierCrossing(t *testing.T) {
	r, _ := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	r.MaxTokens = 1
	for _, tc := range []struct{ input, want int64 }{{1, 1}, {20, 2}, {99999, 6251}} {
		r.InputBound = 99999
		got, err := r.Reserve(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("input %d: cost %d error %v", tc.input, got, err)
		}
	}
	if _, err := r.Reserve(100000); err == nil {
		t.Fatal("accepted upper tier input")
	}
}

func TestRecipe_DocumentPromptVersion(t *testing.T) {
	r, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	old := r
	old.PromptVersion = 1
	if r.PromptVersion != 3 || r.Hash() == old.Hash() || old.Validate() == nil {
		t.Fatalf("current %+v old %+v", r, old)
	}
}

func TestRecipe_HistoricalHashesAndNewDefaults(t *testing.T) {
	base, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if base.PromptVersion != 3 {
		t.Fatalf("document prompt version %d", base.PromptVersion)
	}
	current, err := NewArticleRecipe(base, strings.Repeat("b", 64))
	if err != nil || current.Article.PromptVersion != 5 {
		t.Fatalf("article default %+v: %v", current.Article, err)
	}

	base.PromptVersion = 2
	cases := []struct {
		name    string
		article *ArticleRecipe
		hash    string
	}{
		{"document", nil, "ce48ace939c8a85bb3ef93c87f4ba0424627a14d6a1400f5838680e88a6aa7fa"},
		{"article", &ArticleRecipe{SourceRecipeHash: "ce48ace939c8a85bb3ef93c87f4ba0424627a14d6a1400f5838680e88a6aa7fa", FamiliesHash: strings.Repeat("b", 64), PromptVersion: 1, SchemaVersion: 1, MaxTokens: 8192}, "36ac790077beae175ca3c5ec267afbd86ee9ed2c7f4a8c5c23454c66e5d809e8"},
		{"step one", &ArticleRecipe{SourceRecipeHash: "ce48ace939c8a85bb3ef93c87f4ba0424627a14d6a1400f5838680e88a6aa7fa", FamiliesHash: strings.Repeat("b", 64), PromptVersion: 3, SchemaVersion: 1, MaxTokens: 8192, Step: 1}, "4d09cb44593e0d7adfa63f4de0a1a4424920e550e654244617687bd7d39203fc"},
		{"step two", &ArticleRecipe{SourceRecipeHash: "ce48ace939c8a85bb3ef93c87f4ba0424627a14d6a1400f5838680e88a6aa7fa", FamiliesHash: strings.Repeat("b", 64), PromptVersion: 3, SchemaVersion: 1, MaxTokens: 8192, Step: 2, Step1Hash: strings.Repeat("c", 64)}, "0255258e3ea889511dca9f8d8d5f08eefd8a4b53f240fa8d4024102c60f022d7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Article = tc.article
			if err := r.validate(true); err != nil {
				t.Fatal(err)
			}
			if got := r.Hash(); got != tc.hash {
				t.Fatalf("hash %s, want %s", got, tc.hash)
			}
		})
	}
}
