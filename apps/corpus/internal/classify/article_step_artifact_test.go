package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func step1Fixture(t *testing.T, o Options, labelsHash string, articles []Article) (ArticleArtifact, string, string) {
	t.Helper()
	r := stepRecipe(t, 1)
	a := ArticleArtifact{Version: 1, Country: o.Country, Release: o.Release, Recipe: r, RecipeHash: r.Hash(), TaxonomyHash: r.TaxonomyHash, FamiliesHash: r.Article.FamiliesHash, LabelsHash: labelsHash, Settled: true, InputHash: articleInputHash(o.Country, o.Release, labelsHash, articles)}
	for _, article := range articles {
		a.Articles = append(a.Articles, ArticleRecord{article.DocumentKey, article.Anchor, ArticleOutcome{Outcome: "ok", Codes: []CodeConfidence{{"1.1", 0.8}, {"2.1", 0.7}}}})
	}
	path, hash := publishStep1(t, a)
	return a, path, hash
}

func publishStep1(t *testing.T, a ArticleArtifact) (string, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeOnce(filepath.Join(dir, "articles.json"), b); err != nil {
		t.Fatal(err)
	}
	if err = writeOnce(filepath.Join(dir, "MANIFEST.sha256"), []byte(digest(b)+"  articles.json\n")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "articles.json"), digest(b)
}

func TestArticleSteps_RefusesInvalidStep1Artifacts(t *testing.T) {
	for _, reason := range []string{"manifest", "recipe hash", "unsettled", "wrong step", "wrong source", "release", "labels", "inputs", "missing article", "duplicate article", "foreign article", "foreign family", "deep code", "bound hash", "failed families"} {
		t.Run(reason, func(t *testing.T) {
			_, ledger, client, o, families := runArticlesFixture(t)
			o.Recipe = stepRecipe(t, 2)
			o.DryRun = true
			articles := articleInputs(1, "chapter")
			a, _, _ := step1Fixture(t, o, "labels", articles)
			switch reason {
			case "recipe hash":
				a.RecipeHash = strings.Repeat("f", 64)
			case "unsettled":
				a.Settled = false
			case "wrong step":
				a.Recipe = stepRecipe(t, 2)
				a.RecipeHash = a.Recipe.Hash()
			case "wrong source":
				a.Recipe.Effort = "low"
				a.Recipe.Article.SourceRecipeHash = a.Recipe.documentRecipe().Hash()
				a.RecipeHash = a.Recipe.Hash()
			case "release":
				a.Release = "20990102"
			case "labels":
				a.LabelsHash = "different"
			case "inputs":
				a.InputHash = strings.Repeat("d", 64)
			case "missing article":
				a.Articles = nil
			case "duplicate article":
				a.Articles = append(a.Articles, a.Articles[0])
			case "foreign article":
				a.Articles[0].Anchor = "foreign"
			case "foreign family":
				a.Articles[0].Outcome.Codes = []CodeConfidence{{"2.1", 0.8}}
			case "deep code":
				a.Articles[0].Outcome.Codes = []CodeConfidence{{"1.1.c1", 0.8}}
			case "failed families":
				failed := 1
				a.Articles[0].Outcome.FailedFamilies = &failed
			}
			if reason != "foreign family" {
				families.Documents["vn:vbpl:1"] = Outcome{Outcome: "ok", Codes: []string{"1.1", "2.1"}}
			}
			path, hash := publishStep1(t, a)
			o.Step1Artifact = path
			o.Recipe.Article.Step1Hash = hash
			if reason == "bound hash" {
				o.Recipe.Article.Step1Hash = strings.Repeat("f", 64)
			}
			if reason == "manifest" {
				if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := readArticleChoices(o, "labels", articles, families, articleNodes())
			if err == nil || client.counts != 0 || client.creates != 0 || len(ledger.rows) != 0 {
				t.Fatalf("error %v counts %d creates %d rows %d", err, client.counts, client.creates, len(ledger.rows))
			}
		})
	}
}

func TestArticleSteps_ClosedTrialRejectsNewRequests(t *testing.T) {
	for _, step := range []int{1, 2} {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			runner, ledger, client, o, families := runArticlesFixture(t)
			o.Recipe = stepRecipe(t, step)
			for _, dry := range []bool{true, false} {
				o.DryRun = dry
				if _, err := runner.Articles(context.Background(), o, "labels", articleInputs(1, "chapter"), families, articleNodes()); err == nil {
					t.Fatal("closed trial accepted")
				}
				if client.counts != 0 || client.creates != 0 || len(ledger.rows) != 0 {
					t.Fatal("closed trial reached provider or ledger")
				}
			}
		})
	}
}
