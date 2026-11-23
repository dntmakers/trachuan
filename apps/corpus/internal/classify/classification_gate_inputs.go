package classify

import (
	"bytes"
	"context"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func ScoreClassificationSet(ctx context.Context, db packQueryer, set ClassificationSet) evaluation.ClassificationGate {
	gate := evaluation.ClassificationGate{Status: "invalid", PolicySHA256: set.PolicyHash, TaxonomyVersion: set.Taxonomy}
	labels, err := readGateLabels(set)
	if err != nil {
		gate.Reasons = []string{err.Error()}
		return gate
	}
	for _, ref := range set.References {
		for _, pin := range labels {
			if ref.Slice == pin.slice && (ref.LabelsHash != pin.hash || ref.Provider != pin.provider) {
				gate.Reasons = []string{fmt.Sprintf("%s reference label pin or provider differs", ref.Slice)}
				return gate
			}
		}
	}
	nodes, err := loadNodes(ctx, db)
	if err != nil {
		gate.Status, gate.Reasons = "unavailable", []string{err.Error()}
		return gate
	}
	articles, err := loadGateArticles(ctx, db, labels)
	if err != nil {
		gate.Status, gate.Reasons = "unavailable", []string{err.Error()}
		return gate
	}
	return scoreClassificationReferences(set, labels, articles, nodes)
}

func readGateLabels(set ClassificationSet) ([]gateLabels, error) {
	policy := set.Policy.Document
	if policy.Provider != "openai" || policy.TaxonomyHash != set.Taxonomy || policy.Article != nil || policy.Validate() != nil {
		return nil, fmt.Errorf("invalid reference classifier policy or taxonomy")
	}
	article, err := NewArticleRecipe(policy, digest(nil))
	if err != nil {
		return nil, fmt.Errorf("derive reference article policy: %w", err)
	}
	canonical, err := packArticlePolicy(article)
	if err != nil {
		return nil, fmt.Errorf("encode reference article policy: %w", err)
	}
	if !bytes.Equal(set.Policy.Article, canonical) {
		return nil, fmt.Errorf("reference article policy differs from the document-derived policy")
	}
	baseline := set.Baseline
	if baseline.Country != "vn" || baseline.Country != set.Source.Country || baseline.Release != PackBaselineRelease || !validKey(baseline.InputHash) || !validKey(baseline.RecipeHash) {
		return nil, fmt.Errorf("invalid reference baseline build identity")
	}
	return readClassificationReferenceCatalog(policy.Provider, policy.Model, baseline.Country, baseline.Release)
}

func readClassificationReferenceCatalog(classifier, model, country, release string) ([]gateLabels, error) {
	raw, err := evaluation.VNTaxonomyReferences()
	if err != nil {
		return nil, err
	}
	var out []gateLabels
	for i, data := range raw {
		slice, provider, labels, err := taxonomy.VerifyClassifierScoringInput(classifier, model, data)
		if err != nil {
			return nil, fmt.Errorf("verify embedded reference pin: %w", err)
		}
		if slice != [...]string{"seeded", "multi-family", "post-2023"}[i] || provider != [...]string{"package", "consensus", "consensus"}[i] || labels.Country != country || labels.Release != release || len(labels.Articles) == 0 {
			return nil, fmt.Errorf("invalid embedded %s reference identity", slice)
		}
		out = append(out, gateLabels{slice, provider, digest(data), labels.Selection()})
	}
	return out, nil
}

func loadGateArticles(ctx context.Context, db packQueryer, labels []gateLabels) ([]Article, error) {
	var keys, anchors []string
	for _, slice := range labels {
		for _, label := range slice.labels.Articles {
			keys = append(keys, label.Key)
			anchors = append(anchors, label.Anchor)
		}
	}
	const query = `SELECT d.key,p.anchor,p.text
FROM (SELECT DISTINCT key,anchor FROM unnest($1::text[],$2::text[]) AS ref(key,anchor)) r
JOIN document d ON d.key=r.key JOIN provision p ON p.document_id=d.id AND p.anchor=r.anchor`
	rows, err := db.Query(ctx, query, keys, anchors)
	if err != nil {
		return nil, fmt.Errorf("read target reference articles: %w", err)
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		var article Article
		if err := rows.Scan(&article.DocumentKey, &article.Anchor, &article.OwnText); err != nil {
			return nil, fmt.Errorf("decode target reference article: %w", err)
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read target reference articles: %w", err)
	}
	return articles, nil
}
