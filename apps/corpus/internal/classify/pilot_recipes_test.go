package classify

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPilotRecipes_HistoricalValidationAndHashes(t *testing.T) {
	b, err := os.ReadFile("testdata/pilot-recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID     string `json:"id"`
		Recipe Recipe `json:"recipe"`
		Hash   string `json:"hash"`
	}
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 20 {
		t.Fatalf("got %d recipes", len(rows))
	}
	for _, row := range rows {
		t.Run(row.ID, func(t *testing.T) {
			if err := row.Recipe.validate(true); err != nil {
				t.Fatal(err)
			}
			if got := row.Recipe.Hash(); got != row.Hash {
				t.Fatalf("hash %s want %s", got, row.Hash)
			}
			if err := row.Recipe.Validate(); err == nil {
				t.Fatal("historical recipe accepted for new work")
			}
		})
	}
}

func TestRecipe_OnlyCurrentVersionsAllowNewWork(t *testing.T) {
	base, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if base.PromptVersion != 3 {
		t.Fatalf("document version %d", base.PromptVersion)
	}
	for _, version := range []int{1, 2} {
		old := base
		old.PromptVersion = version
		if old.Validate() == nil {
			t.Fatalf("document version %d accepted", version)
		}
		if _, err = NewArticleRecipe(old, strings.Repeat("b", 64)); err == nil {
			t.Fatalf("article constructed from document version %d", version)
		}
	}
	article, err := NewArticleRecipe(base, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if article.Article.PromptVersion != 5 {
		t.Fatalf("article version %d", article.Article.PromptVersion)
	}
	for _, version := range []int{1, 3, 4} {
		old := article
		copy := *article.Article
		old.Article = &copy
		old.Article.PromptVersion = version
		if version == 3 {
			old.Article.Step = 1
		}
		if old.Validate() == nil {
			t.Fatalf("article version %d accepted", version)
		}
	}
	if _, err = NewTwoStepArticleRecipe(base, strings.Repeat("b", 64), 1, ""); err == nil {
		t.Fatal("new two-step trial accepted")
	}
}

func TestPilotRecipes_TrialFourHasNoRequestBuilder(t *testing.T) {
	b, err := os.ReadFile("testdata/pilot-recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Recipe Recipe `json:"recipe"`
	}
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Recipe.Article == nil || row.Recipe.Article.PromptVersion != 4 {
			continue
		}
		found = true
		req := ArticleRequest{DocumentKey: "vn:vbpl:1", Articles: articleInputs(1, "chapter"), Nodes: articleNodes()[1:4]}
		if _, err = BuildArticleRequest(row.Recipe, req); err == nil || !strings.Contains(err.Error(), "prompt 4") {
			t.Fatalf("historical prompt 4 builder error %v", err)
		}
	}
	if !found {
		t.Fatal("fixture lacks prompt-4 trial")
	}
}
