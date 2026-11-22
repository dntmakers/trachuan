package classify

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
)

func RunOpenAIArticleTrial(ctx context.Context, o OpenAITrialOptions, client *OpenAITrialClient, articles []Article, families FamilyArtifact, nodes []Node) (OpenAITrialSummary, error) {
	if o.Recipe.Article == nil || o.Recipe.Article.SourceRecipeHash != families.RecipeHash || families.RecipeHash != families.Recipe.Hash() || families.TaxonomyHash != o.Recipe.TaxonomyHash {
		return OpenAITrialSummary{}, fmt.Errorf("article trial recipe and families artifact differ")
	}
	if err := ValidateArticleFamilies(articles, families); err != nil {
		return OpenAITrialSummary{}, err
	}
	return runOpenAITrial(ctx, "article", o, client, func(t *openAITrial) error {
		return t.planArticles(ctx, articles, families, nodes)
	})
}

func (t *openAITrial) planArticles(ctx context.Context, articles []Article, families FamilyArtifact, nodes []Node) error {
	r := t.o.Recipe
	bodies := map[[2]string][]byte{}
	plan, err := buildArticlePlan(ctx, r, articles, families.Documents, nodes, func(ctx context.Context, req ArticleRequest) (int64, error) {
		body, countBody, err := BuildOpenAIArticleRequest(r, req)
		if errors.Is(err, ErrOpenAISchemaLimit) {
			return r.InputBound + 1, nil
		}
		if err != nil {
			return 0, err
		}
		n, err := t.count(ctx, body, countBody)
		if err == nil && n <= r.InputBound {
			bodies[[2]string{req.DocumentKey, req.Articles[0].Anchor}] = body
		}
		return n, err
	})
	if err != nil {
		return err
	}
	t.nodes = nodes
	meta := &ArticleMetadata{LabelsHash: t.o.LabelsHash, Skipped: plan.Skipped}
	for i, req := range plan.Requests {
		body := bodies[[2]string{req.DocumentKey, req.Articles[0].Anchor}]
		if len(body) == 0 || t.counts[openAICountKey(body)] != req.InputTokens {
			return fmt.Errorf("article trial plan has no matching counted request")
		}
		item := Item{Key: strconv.Itoa(i), RequestHash: openAICountKey(body), InputTokens: req.InputTokens, Article: &req}
		if i == 0 {
			item.ArticleMeta = meta
		}
		t.result.Items = append(t.result.Items, item)
		t.parts = append(t.parts, openAITrialItem{openaibatch.Item{ID: strconv.Itoa(i), Body: body}, i, req.InputTokens})
	}
	if len(t.parts) == 0 {
		t.result.Items = []Item{{ArticleMeta: meta, Local: true}}
	}
	return nil
}
