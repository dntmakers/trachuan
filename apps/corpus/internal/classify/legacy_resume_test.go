package classify

import (
	"context"
	"strings"
	"testing"
)

func TestLegacyResume_SubmittedAndCompleted(t *testing.T) {
	for _, state := range []string{"submitted", "completed"} {
		t.Run(state, func(t *testing.T) {
			r, ledger, client, options := screenFixture(t)
			old := options
			old.Recipe.Provider = ""
			s := Submission{ID: strings.Repeat("a", 32), RunID: old.RunID, Country: old.Country, Release: old.Release, ScreenHash: screenHash(old), Recipe: old.Recipe, Families: families(), Items: []Item{{Key: documents()[0].Key, InputTokens: 100}}, State: "preparing", CreatedAt: r.Now(), Budget: old.Budget, BudgetMicroUSD: old.BudgetMicroUSD, ReserveMicroUSD: 1029, Results: map[string]Outcome{}}
			s.ItemsHash = itemsHash(s.Items)
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			if err := r.recordBatch(&s, "batch_legacy"); err != nil {
				t.Fatal(err)
			}
			if state == "completed" {
				s.State, s.Settled, s.CostMicroUSD = "completed", true, 8
			}
			ledger.rows = []Submission{s}
			if state == "submitted" {
				other := s
				other.ID = strings.Repeat("b", 32)
				other.ScreenHash = strings.Repeat("c", 64)
				other.State, other.BatchID = "preparing", ""
				other.SubmittedAt, other.DeadlineAt = nil, nil
				if err := r.writeIntent(other); err != nil {
					t.Fatal(err)
				}
				if err := r.recordBatch(&other, "batch_other"); err != nil {
					t.Fatal(err)
				}
				ledger.rows = append(ledger.rows, other)
			}
			if state == "completed" {
				if err := r.Rebuild(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				client.ended = false
			}
			_, err := r.Screen(context.Background(), options, documents(), families())
			if state == "completed" && err != nil {
				t.Fatal(err)
			}
			if state == "submitted" && (err == nil || client.gets == 0) {
				t.Fatalf("expected resumed polling: %v", err)
			}
			if client.creates != 0 {
				t.Fatalf("created %d new batches", client.creates)
			}
		})
	}
}

func TestLegacyResume_ExactOptionsOnly(t *testing.T) {
	_, _, _, options := screenFixture(t)
	legacy := options
	legacy.Recipe.Provider = ""
	for _, selection := range []bool{false, true} {
		current, old := options, legacy
		if selection {
			current.Selection = &DocumentSelection{DocumentKeys: []string{"vn:vbpl:1"}}
			old.Selection = current.Selection
		}
		s := Submission{Recipe: old.Recipe, ScreenHash: screenHash(old)}
		if !matchesScreen(s, screenHash(current), legacyScreenHash(current)) {
			t.Fatal("legacy options did not match")
		}
		current.Seed++
		if selection {
			current.Selection = &DocumentSelection{DocumentKeys: []string{"vn:vbpl:2"}}
		}
		if matchesScreen(s, screenHash(current), legacyScreenHash(current)) {
			t.Fatal("different inputs matched")
		}
	}
	current := options
	current.Recipe = articleRecipe(t)
	current.article = &articleWork{labelsHash: "labels", articles: articleInputs(1, "")}
	old := current
	old.Recipe.Provider = ""
	article := *current.Recipe.Article
	old.Recipe.Article = &article
	article.SourceRecipeHash = old.Recipe.documentRecipe().Hash()
	if legacyScreenHash(current) != screenHash(old) {
		t.Fatal("legacy article identity did not match")
	}
	current.article = &articleWork{labelsHash: "other", articles: articleInputs(1, "")}
	if legacyScreenHash(current) == screenHash(old) {
		t.Fatal("different article inputs matched")
	}
}

func TestLegacyResume_ArticleFamilyAndScore(t *testing.T) {
	labels, families, artifact, release, nodes := scoreFixture(t, 1)
	families.Recipe.Provider = ""
	families.RecipeHash = families.Recipe.Hash()
	recipe, err := NewArticleRecipe(families.Recipe, artifact.FamiliesHash)
	if err != nil {
		t.Fatal(err)
	}
	if recipe.Provider != "anthropic" || recipe.Article.SourceRecipeHash != families.RecipeHash {
		t.Fatal("legacy source identity not preserved")
	}
	artifact.Recipe, artifact.RecipeHash = recipe, recipe.Hash()
	if err = recipe.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err = Score(labels, families, artifact, release, nodes); err != nil {
		t.Fatal(err)
	}
	runner, _, _, options, inputFamilies := runArticlesFixture(t)
	inputFamilies.Recipe.Provider = ""
	inputFamilies.RecipeHash = inputFamilies.Recipe.Hash()
	options.Recipe, err = NewArticleRecipe(inputFamilies.Recipe, artifact.FamiliesHash)
	if err != nil {
		t.Fatal(err)
	}
	options.DryRun = true
	if _, err = runner.Articles(context.Background(), options, "labels", articleInputs(1, ""), inputFamilies, articleNodes()); err != nil {
		t.Fatal(err)
	}
	inputFamilies.RecipeHash = strings.Repeat("f", 64)
	if _, err = runner.Articles(context.Background(), options, "labels", articleInputs(1, ""), inputFamilies, articleNodes()); err == nil {
		t.Fatal("accepted a different family recipe")
	}
}

func TestLegacyResume_ExplicitHistoricalRecipeRejected(t *testing.T) {
	for _, model := range []string{"claude-haiku-5-5", "gpt-6.2-sol"} {
		var r Recipe
		var err error
		if strings.HasPrefix(model, "gpt-") {
			r, err = NewOpenAIRecipe(model, strings.Repeat("a", 64))
		} else {
			r, err = NewRecipe(model, strings.Repeat("a", 64))
		}
		if err != nil {
			t.Fatal(err)
		}
		r.PromptVersion = 2
		if r.validate(true) == nil {
			t.Fatalf("accepted stale explicit recipe %s", model)
		}
		r.PromptVersion = 3
		r.Article = &ArticleRecipe{SourceRecipeHash: r.Hash(), FamiliesHash: strings.Repeat("b", 64), PromptVersion: 1, SchemaVersion: 1, MaxTokens: 8192}
		if r.validate(true) == nil {
			t.Fatalf("accepted stale explicit article recipe %s", model)
		}
	}
}
