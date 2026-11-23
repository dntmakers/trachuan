package classify

import (
	"encoding/hex"
	"fmt"
)

type ArticleRecipe struct {
	SourceRecipeHash string `json:"source_recipe_hash"`
	FamiliesHash     string `json:"families_sha256"`
	PromptVersion    int    `json:"prompt_version"`
	SchemaVersion    int    `json:"schema_version"`
	MaxTokens        int64  `json:"max_tokens"`
	Step             int    `json:"step,omitempty"`
	Step1Hash        string `json:"step_1_sha256,omitempty"`
}

func NewArticleRecipe(base Recipe, familiesHash string) (Recipe, error) {
	if base.Article != nil {
		return Recipe{}, fmt.Errorf("article recipe requires a document recipe")
	}
	source := base.Hash()
	if base.Provider == "" {
		if base.PromptVersion != 3 || base.validate(true) != nil {
			return Recipe{}, fmt.Errorf("invalid historical document recipe")
		}
		base.Provider = "anthropic"
	} else if err := base.Validate(); err != nil {
		return Recipe{}, err
	}
	base.Article = &ArticleRecipe{SourceRecipeHash: source, FamiliesHash: familiesHash, PromptVersion: 5, SchemaVersion: 1, MaxTokens: 8192}
	return base, base.Validate()
}

func (r Recipe) documentRecipe() Recipe { r.Article = nil; return r }

func (r Recipe) validateArticle(historical bool) error {
	a := r.Article
	h, err := hex.DecodeString(a.FamiliesHash)
	base := r.documentRecipe()
	validSource := a.SourceRecipeHash == base.Hash()
	if !validSource && base.Provider == "anthropic" {
		base.Provider = ""
		validSource = a.SourceRecipeHash == base.Hash()
	}
	if err != nil || len(h) != 32 || !validSource || a.SchemaVersion != 1 || a.MaxTokens < 1 || a.MaxTokens > 32000 {
		return fmt.Errorf("invalid article classification recipe")
	}
	if (a.PromptVersion == 5 || (historical && (a.PromptVersion == 1 || a.PromptVersion == 4))) && a.Step == 0 && a.Step1Hash == "" {
		return nil
	}
	if !historical || a.PromptVersion != 3 || r.Model != "claude-haiku-5-5" || (a.Step != 1 && a.Step != 2) {
		return fmt.Errorf("invalid two-step article recipe")
	}
	if a.Step == 1 && a.Step1Hash == "" {
		return nil
	}
	h, err = hex.DecodeString(a.Step1Hash)
	if a.Step != 2 || err != nil || len(h) != 32 {
		return fmt.Errorf("step 2 requires the step-1 artifact hash")
	}
	return nil
}

func NewTwoStepArticleRecipe(_ Recipe, _ string, _ int, _ string) (Recipe, error) {
	return Recipe{}, fmt.Errorf("two-step article classification is a closed historical trial")
}

func (r Recipe) outputTokens() int64 {
	if r.Article != nil {
		return r.Article.MaxTokens
	}
	return r.MaxTokens
}
