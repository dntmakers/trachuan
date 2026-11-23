package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
)

const ClassificationSetNamespace = "classification-set"
const ClassificationSetFilename = "classification.json"

type ClassificationBuild struct {
	Country    string `json:"country"`
	Release    string `json:"release"`
	InputHash  string `json:"build_input_hash"`
	RecipeHash string `json:"build_recipe_hash"`
}

type ClassificationReference struct {
	Slice      string           `json:"slice"`
	Provider   string           `json:"provider"`
	LabelsHash string           `json:"labels_sha256"`
	Documents  []PackedDocument `json:"documents"`
	Articles   []PackedArticle  `json:"articles"`
}

type ClassificationSet struct {
	Version    int                       `json:"version"`
	Source     ClassificationBuild       `json:"source"`
	Taxonomy   string                    `json:"taxonomy_version"`
	Policy     ClassificationPolicy      `json:"policy"`
	PolicyHash string                    `json:"policy_sha256"`
	Documents  []PackedDocument          `json:"documents"`
	Articles   []PackedArticle           `json:"articles"`
	Baseline   ClassificationBuild       `json:"baseline_build"`
	References []ClassificationReference `json:"references"`
}

func (b PackBuild) identity() ClassificationBuild {
	return ClassificationBuild{b.Country, b.Release, b.InputHash, b.RecipeHash}
}

func PackOpenAI(ctx context.Context, runs string, parts int, build PackBuild, baseline PackBaseline, store artifact.Store) (string, error) {
	if len(baseline.references) != 3 {
		return "", fmt.Errorf("pack requires all three pinned baseline references")
	}
	return packOpenAI(ctx, runs, parts, build, baseline.references, store)
}

func packOpenAI(ctx context.Context, runs string, parts int, build PackBuild, references []packReference, store artifact.Store) (string, error) {
	set := ClassificationSet{Version: 1, Source: build.identity(), Taxonomy: build.Taxonomy, Documents: []PackedDocument{}, Articles: []PackedArticle{}}
	seen := map[packRunID]bool{}
	acceptPair := func(pair packPair) error {
		if err := checkPackRunIDs(seen, pair.RunIDs); err != nil {
			return err
		}
		if set.PolicyHash == "" {
			set.Policy, set.PolicyHash = pair.Policy, pair.PolicyHash
		} else if set.PolicyHash != pair.PolicyHash {
			return fmt.Errorf("classification policy drift")
		}
		return nil
	}
	if err := packFullRuns(ctx, runs, parts, build, func(pair packPair) error {
		if err := acceptPair(pair); err != nil {
			return err
		}
		set.Documents = append(set.Documents, pair.Documents...)
		set.Articles = append(set.Articles, pair.Articles...)
		return nil
	}); err != nil {
		return "", err
	}
	if len(references) == 0 {
		return "", fmt.Errorf("missing baseline")
	}
	set.Baseline = references[0].selection.build.identity()
	for _, ref := range references {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if ref.selection.build.Taxonomy != build.Taxonomy || set.Baseline != ref.selection.build.identity() {
			return "", fmt.Errorf("baseline %s taxonomy or build identity mismatch", ref.name)
		}
		pair, err := verifyPackPair(ref.docDir, ref.articleDir, ref.selection)
		if err != nil {
			return "", fmt.Errorf("baseline %s: %w", ref.name, err)
		}
		if err = acceptPair(pair); err != nil {
			return "", fmt.Errorf("baseline %s: %w", ref.name, err)
		}
		set.References = append(set.References, ClassificationReference{ref.name, ref.provider, ref.selection.hash, pair.Documents, pair.Articles})
	}
	sort.Slice(set.References, func(i, j int) bool { return set.References[i].Slice < set.References[j].Slice })
	for i, ref := range set.References {
		if i > 0 && ref.Slice == set.References[i-1].Slice {
			return "", fmt.Errorf("duplicate baseline reference slice")
		}
	}
	if err := sortPackRecords(set.Documents, set.Articles); err != nil {
		return "", err
	}
	raw, err := json.Marshal(set)
	if err != nil {
		return "", fmt.Errorf("encode classification set: %w", err)
	}
	return writePackSet(ctx, store, raw)
}

func packFullRuns(ctx context.Context, runs string, parts int, b PackBuild, accept func(packPair) error) error {
	if parts < 1 || parts > 64 {
		return fmt.Errorf("pack parts must be between 1 and 64")
	}
	root, err := openPackDir(runs)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var work []struct {
		stem string
		sel  packSelection
	}
	want := map[string]bool{}
	for i := 1; i <= parts; i++ {
		sel, selectionErr := makePackSelection(b, i, parts)
		if selectionErr != nil {
			return selectionErr
		}
		if len(sel.articles) == 0 {
			continue
		}
		stem := fmt.Sprintf("n%d-of-%d", i, parts)
		want[stem+"-documents"], want[stem+"-articles"] = true, true
		work = append(work, struct {
			stem string
			sel  packSelection
		}{stem, sel})
	}
	names, err := root.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("list trial parts: %w", err)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
		if strings.HasPrefix(name, "n") && strings.Contains(name, "-of-") && (strings.HasSuffix(name, "-documents") || strings.HasSuffix(name, "-articles")) && !want[name] {
			return fmt.Errorf("unexpected trial part %s", filepath.Join(runs, name))
		}
	}
	for _, part := range work {
		for _, suffix := range []string{"-documents", "-articles"} {
			name := part.stem + suffix
			if !seen[name] {
				return fmt.Errorf("missing trial part %s", filepath.Join(runs, name))
			}
		}
	}
	for _, part := range work {
		if err := ctx.Err(); err != nil {
			return err
		}
		pair, err := root.verifyPair(part.stem, part.sel)
		if err != nil {
			return err
		}
		if err = accept(pair); err != nil {
			return fmt.Errorf("part %s: %w", part.stem, err)
		}
	}
	return nil
}

func (d packDir) verifyPair(stem string, sel packSelection) (packPair, error) {
	doc, err := d.child(stem + "-documents")
	if err != nil {
		return packPair{}, err
	}
	defer func() { _ = doc.Close() }()
	articles, err := d.child(stem + "-articles")
	if err != nil {
		return packPair{}, err
	}
	defer func() { _ = articles.Close() }()
	return verifyPackDirectories(doc, articles, sel)
}

func sortPackRecords(documents []PackedDocument, articles []PackedArticle) error {
	sort.Slice(documents, func(i, j int) bool { return documents[i].Key < documents[j].Key })
	for i, doc := range documents {
		if i > 0 && documents[i-1].Key == doc.Key {
			return fmt.Errorf("duplicate packed document key")
		}
	}
	sort.Slice(articles, func(i, j int) bool {
		a, b := articles[i], articles[j]
		return a.Key < b.Key || a.Key == b.Key && a.Anchor < b.Anchor
	})
	for i, a := range articles {
		if i > 0 && articles[i-1].Key == a.Key && articles[i-1].Anchor == a.Anchor {
			return fmt.Errorf("duplicate packed article key and anchor")
		}
	}
	return nil
}
