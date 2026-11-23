package classify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type Recipe struct {
	Version       int            `json:"version"`
	Model         string         `json:"model"`
	Effort        string         `json:"effort"`
	MaxTokens     int64          `json:"max_tokens"`
	InputBound    int64          `json:"input_bound"`
	F             int            `json:"families"`
	PromptVersion int            `json:"prompt_version"`
	SchemaVersion int            `json:"schema_version"`
	TaxonomyHash  string         `json:"taxonomy_hash"`
	Article       *ArticleRecipe `json:"article,omitempty"`
	Provider      string         `json:"provider,omitempty"`
}

func NewRecipe(model, taxonomyHash string) (Recipe, error) {
	r := Recipe{Provider: "anthropic", Version: 1, Model: model, Effort: "medium", MaxTokens: 4096, InputBound: 90000, F: 5, PromptVersion: 3, SchemaVersion: 2, TaxonomyHash: taxonomyHash}
	return r, r.Validate()
}

func NewOpenAIRecipe(model, taxonomyHash string) (Recipe, error) {
	r := Recipe{Provider: "openai", Version: 1, Model: model, Effort: "medium", MaxTokens: 4096, InputBound: 90000, F: 5, PromptVersion: 3, SchemaVersion: 2, TaxonomyHash: taxonomyHash}
	return r, r.Validate()
}

func (r Recipe) Validate() error {
	return r.validate(false)
}

func (r Recipe) validate(historical bool) error {
	historical = historical && r.Provider == ""
	switch r.Provider {
	case "", "anthropic":
		if r.Provider == "" && !historical {
			return fmt.Errorf("explicit classification provider required")
		}
		if _, ok := priceForModel(r.Model); !ok {
			return fmt.Errorf("unsupported Anthropic model")
		}
	case "openai":
		if _, ok := openAIPriceForModel(r.Model); !ok || r.MaxTokens < 16 || r.outputTokens() < 16 {
			return fmt.Errorf("unsupported OpenAI model or output bound")
		}
	default:
		return fmt.Errorf("unsupported classification provider")
	}
	if r.Effort != "low" && r.Effort != "medium" && r.Effort != "high" && r.Effort != "xhigh" && r.Effort != "max" {
		return fmt.Errorf("invalid classification effort")
	}
	if r.Version != 1 || (r.PromptVersion < 1 || r.PromptVersion > 3 || (!historical && r.PromptVersion != 3)) || r.SchemaVersion != 2 || r.MaxTokens < 1 || r.MaxTokens > 32000 || r.InputBound < 1 || r.InputBound >= 100000 || r.F < 1 {
		return fmt.Errorf("invalid classification recipe")
	}
	h, err := hex.DecodeString(r.TaxonomyHash)
	if err != nil || len(h) != 32 {
		return fmt.Errorf("invalid taxonomy hash")
	}
	if r.Article != nil {
		if err := r.validateArticle(historical); err != nil {
			return err
		}
	}
	return nil
}

func (r Recipe) Hash() string {
	b, _ := json.Marshal(r)
	return digest(b)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
