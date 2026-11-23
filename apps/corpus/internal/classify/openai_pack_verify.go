package classify

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

const PackBaselineRelease = "20261115_3"

type packSelection struct {
	build     PackBuild
	articles  []taxonomy.LabelArticle
	hash      string
	partition string
}

type packPair struct {
	Documents  []PackedDocument
	Articles   []PackedArticle
	Policy     ClassificationPolicy
	PolicyHash string
	RunIDs     []packRunID
}

type packRunID struct{ field, value string }

func checkPackRunIDs(seen map[packRunID]bool, ids []packRunID) error {
	for _, id := range ids {
		if id.value == "" {
			continue
		}
		if seen[id] {
			return fmt.Errorf("state.json %s was reused across trials", id.field)
		}
		seen[id] = true
	}
	return nil
}

func verifyPackPair(docDir, articleDir string, selection packSelection) (packPair, error) {
	var pair packPair
	parent := filepath.Dir(docDir)
	if parent != filepath.Dir(articleDir) {
		return pair, fmt.Errorf("trial pair must share a parent directory")
	}
	root, err := openPackDir(parent)
	if err != nil {
		return pair, err
	}
	defer func() { _ = root.Close() }()
	docs, err := root.child(filepath.Base(docDir))
	if err != nil {
		return pair, err
	}
	defer func() { _ = docs.Close() }()
	articles, err := root.child(filepath.Base(articleDir))
	if err != nil {
		return pair, err
	}
	defer func() { _ = articles.Close() }()
	return verifyPackDirectories(docs, articles, selection)
}

func verifyPackDirectories(docs, articles packDir, selection packSelection) (packPair, error) {
	var pair packPair
	for _, d := range []packDir{docs, articles} {
		if err := d.lock(); err != nil {
			return pair, fmt.Errorf("lock %s: %w", d.Name(), err)
		}
	}
	familiesRaw, familiesHash, err := docs.output("families.json")
	if err != nil {
		return pair, fmt.Errorf("%s: %w", docs.Name(), err)
	}
	doc, err := docs.verifyTrial("document", selection, func(t *openAITrial) error {
		if planErr := verifyPackDocuments(t, selection); planErr != nil {
			return planErr
		}
		return docs.verifyLocalDocumentCounts(t.state, t.result.Items)
	})
	if err != nil {
		return pair, fmt.Errorf("%s: %w", docs.Name(), err)
	}
	families := FamilyArtifact{1, doc.state.RecipeHash, selection.build.Taxonomy, doc.o.Recipe, doc.result.Results, doc.result.Items[0].Selection}
	if !packSameRecord(familiesRaw, families) {
		return pair, fmt.Errorf("%s: families.json does not match verified recipe, selection and replay", docs.Name())
	}
	articlesRaw, _, err := articles.output("articles.json")
	if err != nil {
		return pair, fmt.Errorf("%s: %w", articles.Name(), err)
	}
	art, err := articles.verifyTrial("article", selection, func(t *openAITrial) error {
		if doc.state.Project != t.state.Project || doc.state.Fingerprint != t.state.Fingerprint {
			return fmt.Errorf("state.json project or key_fingerprint differs from document stage")
		}
		if t.o.Recipe.Article.SourceRecipeHash != families.RecipeHash || t.o.Recipe.Article.FamiliesHash != familiesHash {
			return fmt.Errorf("plan.json article recipe source_recipe_hash or families_sha256 differs from verified families.json")
		}
		return verifyPackArticles(t, selection, families.Documents)
	})
	if err != nil {
		return pair, fmt.Errorf("%s: %w", articles.Name(), err)
	}
	outcome, err := comparePackArticles(art, articlesRaw)
	if err != nil {
		return pair, fmt.Errorf("%s: %w", articles.Name(), err)
	}
	pair, err = packPairFromReplay(doc, art, outcome, selection)
	if err != nil {
		return pair, err
	}
	return pair, checkPackRunIDs(map[packRunID]bool{}, pair.RunIDs)
}

func (d packDir) verifyTrial(stage string, sel packSelection, match func(*openAITrial) error) (*openAITrial, error) {
	t := &openAITrial{}
	raw, err := d.required("state.json")
	if err != nil {
		return nil, err
	}
	if packCheckJSON(raw) != nil || decodeTrialState(raw, &t.state) != nil {
		return nil, fmt.Errorf("invalid state.json")
	}
	if err = verifyPackState(t.state, stage, sel); err != nil {
		return nil, err
	}
	planRaw, err := d.bound("plan.json", t.state.PlanHash)
	if err != nil {
		return nil, err
	}
	var plan openAITrialPlan
	if err = decodePackJSON("plan.json", planRaw, &plan); err != nil {
		return nil, err
	}
	if err = verifyPackRecipe(t.state, plan.Recipe, sel.build.Taxonomy); err != nil {
		return nil, err
	}
	input, err := d.bound("input.jsonl", t.state.InputHash)
	if err != nil {
		return nil, err
	}
	if err = t.restoreBoundPlan(plan, input, true); err != nil {
		return nil, fmt.Errorf("plan.json and input.jsonl: %w", err)
	}
	if err = match(t); err != nil {
		return nil, err
	}
	if err = d.verifyCost(t); err != nil {
		return nil, err
	}
	return t, nil
}

func verifyPackState(s openAITrialState, stage string, sel packSelection) error {
	if s.Phase != trialSettled || s.UnpricedResults != 0 {
		return fmt.Errorf("state.json must have phase=settled and zero unpriced_results")
	}
	if s.Country != sel.build.Country || s.Release != sel.build.Release || s.Stage != stage {
		return fmt.Errorf("state.json country, release or stage differs from expected identity")
	}
	if s.LabelsHash != sel.hash {
		return fmt.Errorf("state.json labels_sha256 differs from source selection")
	}
	opts := OpenAITrialOptions{Unseeded: sel.partition != "", Selection: sel.partition}
	if !s.matchesSelection(opts) {
		return fmt.Errorf("state.json selection_mode or selection differs from source selection")
	}
	if opts.Unseeded && (s.SelectedArticles != len(sel.articles) || s.SelectedDocuments != len(sel.documentKeys())) || !opts.Unseeded && (s.SelectedArticles != 0 || s.SelectedDocuments != 0) {
		return fmt.Errorf("state.json selected_documents or selected_articles differs from source selection")
	}
	return nil
}

func verifyPackRecipe(s openAITrialState, r Recipe, version string) error {
	if r.TaxonomyHash != version || r.Hash() != s.RecipeHash {
		return fmt.Errorf("plan.json recipe taxonomy_hash or state.json recipe_sha256 mismatch")
	}
	expected, err := NewOpenAIRecipe("gpt-6.2-sol", version)
	if err != nil {
		return err
	}
	if r.Article != nil {
		expected, err = NewArticleRecipe(expected, r.Article.FamiliesHash)
		if err != nil {
			return err
		}
	}
	if r.Hash() != expected.Hash() {
		return fmt.Errorf("plan.json recipe differs from selected gpt-6.2-sol recipe")
	}
	return nil
}

func (d packDir) verifyCost(t *openAITrial) error {
	replayed := openAITrialCost{Items: []openAITrialCostItem{}, Unpriced: []string{}, Complete: true}
	if len(t.parts) == 0 {
		if t.state.ReserveMicroUSD != 0 || t.state.BatchID != "" {
			return fmt.Errorf("local-only trial has nonzero reserve or batch_id in state.json")
		}
		if err := d.noBatchEvidence(); err != nil {
			return err
		}
	} else {
		batch, capture, err := d.receipts(t.state)
		if err != nil {
			return err
		}
		lines, err := replayOpenAITrialCoverage(t.parts, t.state.Stage, batch, capture)
		if err != nil || batch.Status == "cancelled" {
			return fmt.Errorf("%s and result captures do not prove batch coverage", t.state.TerminalCapture)
		}
		replayed = t.price(lines, true, batch)
	}
	if !replayed.Complete || len(replayed.Unpriced) != 0 {
		return fmt.Errorf("result captures do not prove complete pricing: %s", replayed.Issue)
	}
	raw, err := d.required("cost.json")
	if err != nil {
		return err
	}
	var recorded openAITrialCost
	if err = decodePackJSON("cost.json", raw, &recorded); err != nil {
		return err
	}
	if !packSameRecord(raw, replayed) {
		return fmt.Errorf("cost.json differs from replayed coverage and pricing")
	}
	if t.state.CostMicroUSD != replayed.PricedMicroUSD {
		return fmt.Errorf("state.json cost_micro_usd differs from replayed pricing")
	}
	return nil
}

func (s packSelection) documentKeys() []string {
	keys := make([]string, 0)
	for _, article := range s.articles {
		keys = append(keys, article.Key)
	}
	sort.Strings(keys)
	return slices.Compact(keys)
}
