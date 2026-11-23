package build

import (
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

type classificationArticleKey struct{ key, anchor string }

type classificationOutcome struct {
	ownTextSHA256 string
	reason        string
	rows          map[int64]float64
}

type classificationMatcher map[classificationArticleKey]classificationOutcome

func (m classificationMatcher) match(key, anchor, text string) (map[int64]float64, string) {
	outcome, found := m[classificationArticleKey{key, anchor}]
	if !found {
		return nil, "missing_result"
	}
	if outcome.ownTextSHA256 != taxonomy.OwnTextSHA256(text) {
		return nil, "changed_own_text"
	}
	return outcome.rows, outcome.reason
}

func newClassificationMatcher(set classify.ClassificationSet, nodes map[string]taxonomyNode) (classificationMatcher, error) {
	families := make(map[string]int64, len(set.Policy.Menu))
	for _, entry := range set.Policy.Menu {
		n := nodes[entry.Code]
		if n.status != "active" || n.level != 2 || n.family != n.id || families[entry.Code] != 0 {
			return nil, fmt.Errorf("invalid classification menu family %q", entry.Code)
		}
		families[entry.Code] = n.id
	}
	for _, ref := range set.References {
		if _, err := indexClassificationEvidence(ref.Documents, ref.Articles, nodes, families); err != nil {
			return nil, fmt.Errorf("invalid classification reference %q: %w", ref.Slice, err)
		}
	}
	return indexClassificationEvidence(set.Documents, set.Articles, nodes, families)
}

type classificationDocument struct {
	outcome  string
	families map[int64]bool
}

func indexClassificationEvidence(documents []classify.PackedDocument, articles []classify.PackedArticle, nodes map[string]taxonomyNode, families map[string]int64) (classificationMatcher, error) {
	docs := make(map[string]classificationDocument, len(documents))
	for _, doc := range documents {
		if doc.Key == "" || !classify.ValidClassificationSetDigest(doc.RequestHash) || docs[doc.Key].outcome != "" {
			return nil, fmt.Errorf("invalid or duplicate packed document %q", doc.Key)
		}
		switch doc.Outcome {
		case "ok", "no_family", "invalid_response":
		default:
			return nil, fmt.Errorf("invalid packed document outcome at %q", doc.Key)
		}
		if (doc.Outcome == "ok") != (len(doc.Codes) > 0) {
			return nil, fmt.Errorf("inconsistent document families at %q", doc.Key)
		}
		entry := classificationDocument{outcome: doc.Outcome, families: make(map[int64]bool, len(doc.Codes))}
		for _, code := range doc.Codes {
			id := families[code]
			if id == 0 || entry.families[id] {
				return nil, fmt.Errorf("invalid document family %q at %q", code, doc.Key)
			}
			entry.families[id] = true
		}
		docs[doc.Key] = entry
	}
	m := make(classificationMatcher, len(articles))
	for _, article := range articles {
		key := classificationArticleKey{article.Key, article.Anchor}
		if _, exists := m[key]; exists || article.Anchor == "" || !classify.ValidClassificationSetDigest(article.OwnTextSHA256) {
			return nil, fmt.Errorf("invalid or duplicate packed article at %q %q", key.key, key.anchor)
		}
		doc := docs[article.Key]
		if doc.outcome == "" || (doc.outcome != "ok" && doc.outcome != article.Outcome) {
			return nil, fmt.Errorf("inconsistent document outcome at %q %q", key.key, key.anchor)
		}
		outcome, err := validatedClassificationOutcome(article, doc, nodes)
		if err != nil {
			return nil, fmt.Errorf("validate packed article %q %q: %w", key.key, key.anchor, err)
		}
		m[key] = outcome
	}
	return m, nil
}

func validatedClassificationOutcome(article classify.PackedArticle, doc classificationDocument, nodes map[string]taxonomyNode) (classificationOutcome, error) {
	outcome := classificationOutcome{ownTextSHA256: article.OwnTextSHA256}
	switch article.Outcome {
	case "no_family", "invalid_response", "too_long":
		if len(article.Codes) != 0 {
			return outcome, fmt.Errorf("failed outcome %q carries codes", article.Outcome)
		}
		outcome.reason = article.Outcome
	case "ok":
		if len(article.Codes) == 0 {
			outcome.reason = "no_code"
			return outcome, nil
		}
		outcome.rows = make(map[int64]float64)
		seen := make(map[string]bool, len(article.Codes))
		for _, p := range article.Codes {
			n := nodes[p.Code]
			if n.status != "active" || !doc.families[n.family] || seen[p.Code] || !(p.Confidence >= 0 && p.Confidence <= 1) {
				return outcome, fmt.Errorf("invalid article code, family or confidence for %q", p.Code)
			}
			seen[p.Code] = true
			outcome.rows[n.family] = max(outcome.rows[n.family], p.Confidence)
		}
	default:
		return outcome, fmt.Errorf("unknown article outcome %q", article.Outcome)
	}
	return outcome, nil
}
