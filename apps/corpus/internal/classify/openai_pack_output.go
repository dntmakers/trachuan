package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type ClassificationPolicy struct {
	Document Recipe          `json:"document_recipe"`
	Article  json.RawMessage `json:"article_recipe"`
	Menu     []Family        `json:"document_menu"`
}

type PackedDocument struct {
	Key         string   `json:"document_key"`
	RequestHash string   `json:"request_hash"`
	Outcome     string   `json:"outcome"`
	Codes       []string `json:"codes"`
}

type PackedArticle struct {
	Key           string           `json:"document_key"`
	Anchor        string           `json:"anchor"`
	OwnTextSHA256 string           `json:"own_text_sha256"`
	Outcome       string           `json:"outcome"`
	Codes         []CodeConfidence `json:"codes"`
}

func comparePackArticles(t *openAITrial, raw []byte) (ArticleArtifact, error) {
	want, err := renderArticleArtifact(t.result)
	if err != nil {
		return ArticleArtifact{}, err
	}
	if !packSameRecord(raw, want) {
		return ArticleArtifact{}, fmt.Errorf("articles.json does not match verified recipe, selection and replay")
	}
	return want, nil
}

func packPairFromReplay(doc, art *openAITrial, output ArticleArtifact, selection packSelection) (packPair, error) {
	policy, hash, err := packPolicy(doc.o.Recipe, art.o.Recipe, doc.result.Families)
	if err != nil {
		return packPair{}, err
	}
	p := packPair{Policy: policy, PolicyHash: hash}
	for _, state := range []openAITrialState{doc.state, art.state} {
		p.RunIDs = append(p.RunIDs, packRunID{"batch_id", state.BatchID}, packRunID{"submission_id", state.SubmissionID})
	}
	for _, item := range doc.result.Items {
		out := doc.result.Results[item.Key]
		p.Documents = append(p.Documents, PackedDocument{item.Key, item.RequestHash, out.Outcome, out.Codes})
	}
	triples := make(map[packArticleKey]string, len(selection.articles))
	for _, a := range selection.articles {
		triples[packArticleKey{a.Key, a.Anchor}] = a.OwnTextSHA256
	}
	for _, a := range output.Articles {
		p.Articles = append(p.Articles, PackedArticle{a.DocumentKey, a.Anchor, triples[packArticleKey{a.DocumentKey, a.Anchor}], a.Outcome.Outcome, a.Outcome.Codes})
	}
	return p, nil
}

func packPolicy(document, article Recipe, menu []Family) (ClassificationPolicy, string, error) {
	raw, err := packArticlePolicy(article)
	if err != nil {
		return ClassificationPolicy{}, "", err
	}
	p := ClassificationPolicy{document, raw, menu}
	hash, err := p.Hash()
	return p, hash, err
}

func packArticlePolicy(article Recipe) (json.RawMessage, error) {
	if article.Article == nil {
		return nil, fmt.Errorf("missing article recipe")
	}
	raw, err := json.Marshal(article)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	var stage map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(fields["article"], &stage); err != nil {
		return nil, err
	}
	delete(stage, "families_sha256")
	if fields["article"], err = json.Marshal(stage); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func (p ClassificationPolicy) Hash() (string, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(p.Article), []byte("{")) || packCheckJSON(p.Article) != nil {
		return "", fmt.Errorf("classification article policy must be a JSON object")
	}
	tuple, err := json.Marshal([3]any{p.Document, p.Article, p.Menu})
	if err != nil {
		return "", fmt.Errorf("encode classification policy: %w", err)
	}
	return digest(tuple), nil
}
