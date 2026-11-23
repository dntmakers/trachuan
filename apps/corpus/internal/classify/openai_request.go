package classify

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

type openAIContent struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	Breakpoint any    `json:"prompt_cache_breakpoint,omitempty"`
}
type openAIMessage struct {
	Role    string          `json:"role"`
	Content []openAIContent `json:"content"`
}
type openAIRequest struct {
	Model              string            `json:"model"`
	Input              []openAIMessage   `json:"input"`
	Reasoning          map[string]string `json:"reasoning"`
	Text               any               `json:"text"`
	Truncation         string            `json:"truncation"`
	MaxOutputTokens    int64             `json:"max_output_tokens"`
	Store              bool              `json:"store"`
	PromptCacheOptions map[string]string `json:"prompt_cache_options"`
}

func openAIRecipe(r Recipe) (Recipe, error) {
	if r.Provider != "openai" {
		return Recipe{}, fmt.Errorf("explicit OpenAI provider required")
	}
	if err := r.Validate(); err != nil {
		return Recipe{}, fmt.Errorf("invalid OpenAI recipe: %w", err)
	}
	if r.Model != "gpt-6.2-sol" && r.Model != "gpt-6.2-astra" && r.Model != "gpt-6.1-terra" && r.Model != "gpt-6-luna" {
		return Recipe{}, fmt.Errorf("unsupported OpenAI model")
	}
	if r.Effort != "low" && r.Effort != "medium" && r.Effort != "high" && r.Effort != "xhigh" && r.Effort != "max" {
		return Recipe{}, fmt.Errorf("unsupported OpenAI effort")
	}
	if r.PromptVersion != 3 {
		return Recipe{}, fmt.Errorf("unsupported OpenAI prompt version")
	}
	if r.Article != nil {
		if r.Article.SourceRecipeHash != r.documentRecipe().Hash() || r.Article.PromptVersion != 5 {
			return Recipe{}, fmt.Errorf("invalid OpenAI article recipe")
		}
		article := *r.Article
		r.Article = &article
	}
	r.Model = "claude-haiku-5-5"
	r.Provider = "anthropic"
	if r.Article != nil {
		r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	}
	if err := r.validate(false); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

func BuildOpenAIDocumentRequest(r Recipe, d Document, families []Family) ([]byte, []byte, error) {
	if r.Article != nil || r.MaxTokens < 16 {
		return nil, nil, fmt.Errorf("invalid document recipe or output limit")
	}
	surrogate, err := openAIRecipe(r)
	if err != nil {
		return nil, nil, err
	}
	p, err := BuildRequest(surrogate, d, families)
	if err != nil {
		return nil, nil, err
	}
	return renderOpenAI(r, p, "document_classification")
}

func BuildOpenAIArticleRequest(r Recipe, req ArticleRequest) ([]byte, []byte, error) {
	surrogate, err := openAIRecipe(r)
	if err != nil {
		return nil, nil, err
	}
	if surrogate.Article == nil || surrogate.Article.MaxTokens < 16 {
		return nil, nil, fmt.Errorf("article recipe required")
	}
	if err = openAIArticleLimit(req.Articles); err != nil {
		return nil, nil, err
	}
	p, err := BuildArticleRequest(surrogate, req)
	if err != nil {
		return nil, nil, err
	}
	return renderOpenAI(r, p, "article_classification")
}

func renderOpenAI(r Recipe, p anthropic.MessageNewParams, name string) ([]byte, []byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, nil, fmt.Errorf("encode source request: %w", err)
	}
	var source struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Messages []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		Output struct {
			Format struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		return nil, nil, fmt.Errorf("decode source request: %w", err)
	}
	if len(source.System) != 2 || len(source.Messages) != 1 || len(source.Messages[0].Content) != 1 || len(source.Output.Format.Schema) == 0 {
		return nil, nil, fmt.Errorf("source request cannot be represented")
	}
	request := openAIRequestContract(r, name, source.System[0].Text, source.System[1].Text, source.Messages[0].Content[0].Text, source.Output.Format.Schema, p.MaxTokens)
	body, err := json.Marshal(request)
	if err != nil {
		return nil, nil, fmt.Errorf("encode OpenAI request: %w", err)
	}
	var count map[string]json.RawMessage
	if err = json.Unmarshal(body, &count); err != nil {
		return nil, nil, fmt.Errorf("decode OpenAI request: %w", err)
	}
	delete(count, "max_output_tokens")
	delete(count, "store")
	delete(count, "prompt_cache_options")
	countBody, err := json.Marshal(count)
	if err != nil {
		return nil, nil, fmt.Errorf("encode OpenAI count: %w", err)
	}
	return body, countBody, nil
}

func openAIRequestContract(r Recipe, name, instruction, menu, payload string, schema any, limit int64) openAIRequest {
	return openAIRequest{Model: r.Model, Input: []openAIMessage{{Role: "developer", Content: []openAIContent{{Type: "input_text", Text: instruction}, {Type: "input_text", Text: menu, Breakpoint: map[string]string{"mode": "explicit", "ttl": "30m"}}}}, {Role: "user", Content: []openAIContent{{Type: "input_text", Text: payload}}}}, Reasoning: map[string]string{"effort": r.Effort}, Text: map[string]any{"format": map[string]any{"type": "json_schema", "name": name, "strict": true, "schema": schema}}, Truncation: "disabled", MaxOutputTokens: limit, Store: false, PromptCacheOptions: map[string]string{"mode": "explicit", "ttl": "30m"}}
}
