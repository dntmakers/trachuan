package classify

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAITrialArticles_ReadsExistingScoreShape(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	ctx := t.Context()
	if _, err := RunOpenAIDocumentTrial(ctx, o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	families, familiesHash, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil {
		t.Fatal(err)
	}
	o.Recipe, err = NewArticleRecipe(families.Recipe, familiesHash)
	if err != nil {
		t.Fatal(err)
	}
	o.Out = filepath.Join(filepath.Dir(o.Out), "articles")
	wire.out, wire.calls = o.Out, nil
	wire.result = `{"a1":{"codes":[{"code":"F","confidence":0.9},{"code":"foreign","confidence":0.8}]},"unrequested":{"codes":[]}}`
	articles := []Article{{DocumentKey: docs[0].Key, Anchor: "a1", Text: "Điều 1"}, {DocumentKey: docs[0].Key, Anchor: "a2", Text: "Điều 2"}}
	o.DryRun = true
	summary, err := RunOpenAIArticleTrial(ctx, o, wire.client(), articles, families, nodes)
	if err != nil || summary.Requests != 1 || summary.CountedInputTokens != 100 || summary.SiblingMicroUSD != 151 || !reflect.DeepEqual(wire.calls, []string{"POST /v1/responses/input_tokens", "POST /v1/responses/input_tokens"}) {
		t.Fatalf("article dry run = %+v err=%v calls=%v", summary, err, wire.calls)
	}
	o.DryRun = false
	if _, err = RunOpenAIArticleTrial(ctx, o, wire.client(), articles, families, nodes); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /v1/responses/input_tokens", "POST /v1/responses/input_tokens", "POST /v1/files", "POST /v1/batches", "GET /v1/batches/batch-one", "GET /v1/files/file-result/content"}
	if !reflect.DeepEqual(wire.calls, want) {
		t.Fatalf("article calls = %v", wire.calls)
	}
	a, err := ReadArticleArtifactForScore(filepath.Join(o.Out, "articles.json"), o.Recipe.TaxonomyHash, familiesHash)
	if err != nil || a.Recipe.Article.SourceRecipeHash != families.RecipeHash || a.LabelsHash != o.LabelsHash || a.DroppedAnchors != 1 || len(a.Articles) != 2 {
		t.Fatalf("article artifact = %+v, %v", a, err)
	}
	if first, second := a.Articles[0], a.Articles[1]; first.Anchor != "a1" || !reflect.DeepEqual(first.Outcome.Codes, []CodeConfidence{{Code: "F", Confidence: 0.9}}) || !reflect.DeepEqual(first.Outcome.Dropped, []string{"foreign"}) || second.Anchor != "a2" || second.Outcome.Outcome != "invalid_response" {
		t.Fatalf("unexpected article outcomes: %+v", a.Articles)
	}
}

func TestOpenAITrialArticles_AllLocal(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	wire.result = `{"codes":[]}`
	ctx := t.Context()
	if _, err := RunOpenAIDocumentTrial(ctx, o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	families, familiesHash, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil {
		t.Fatal(err)
	}
	o.Recipe, err = NewArticleRecipe(families.Recipe, familiesHash)
	if err != nil {
		t.Fatal(err)
	}
	o.Out = filepath.Join(filepath.Dir(o.Out), "articles")
	wire.out, wire.calls = o.Out, nil
	articles := []Article{{DocumentKey: docs[0].Key, Anchor: "a1", Text: "Điều 1"}}
	summary, err := RunOpenAIArticleTrial(ctx, o, wire.client(), articles, families, nodes)
	if err != nil || summary.Requests != 0 || summary.ReserveMicroUSD != 0 || len(wire.calls) != 0 {
		t.Fatalf("local article run = %+v err=%v calls=%v", summary, err, wire.calls)
	}
	a, err := ReadArticleArtifactForScore(filepath.Join(o.Out, "articles.json"), o.Recipe.TaxonomyHash, familiesHash)
	if err != nil || len(a.Articles) != 1 || a.Articles[0].Outcome.Outcome != "no_family" {
		t.Fatalf("local article artifact = %+v, %v", a, err)
	}
}

func TestOpenAITrialArticles_ResumeRejectsDifferentFamiliesHash(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	ctx := t.Context()
	if _, err := RunOpenAIDocumentTrial(ctx, o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	families, hash, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil {
		t.Fatal(err)
	}
	o.Out = filepath.Join(filepath.Dir(o.Out), "articles")
	o.Recipe, err = NewArticleRecipe(families.Recipe, hash)
	if err != nil {
		t.Fatal(err)
	}
	wire.out = o.Out
	wire.pause = "GET /v1/batches/batch-one"
	articles := []Article{{DocumentKey: docs[0].Key, Anchor: "a1", Text: "Điều 1"}}
	if _, err := RunOpenAIArticleTrial(ctx, o, wire.client(), articles, families, nodes); err == nil {
		t.Fatal("expected interrupted retrieve")
	}
	wire.calls = nil
	o.Recipe = families.Recipe
	o.ExpectedFamiliesHash = strings.Repeat("c", 64)
	if _, handled, err := ResumeOpenAITrial(ctx, "articles", o, wire.client()); !handled || err == nil || len(wire.calls) != 0 {
		t.Fatalf("accepted different families on resume: handled=%t calls=%v err=%v", handled, wire.calls, err)
	}
}
