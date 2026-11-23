package classify

import (
	"fmt"
	"strconv"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

type gateLabels struct {
	slice, provider, hash string
	labels                taxonomy.Labels
}

type gateFloor struct {
	metric  string
	percent int
}

var classificationFloors = [...]struct {
	slice  string
	floors [3]gateFloor
}{
	{"seeded", [3]gateFloor{{"recall", 85}, {"top_1_precision", 80}, {"family_recall", 90}}},
	{"multi-family", [3]gateFloor{{"recall", 85}, {"precision", 70}, {"family_recall", 90}}},
	{"post-2023", [3]gateFloor{{"recall", 85}, {"precision", 70}, {"family_recall", 90}}},
}

func ClassificationFloorsHash() string {
	var parts []string
	for _, row := range classificationFloors {
		for _, floor := range row.floors {
			parts = append(parts, row.slice, floor.metric, strconv.Itoa(floor.percent))
		}
	}
	return digest([]byte(strings.Join(parts, "\x00")))
}

func scoreClassificationReferences(set ClassificationSet, labels []gateLabels, articles []Article, nodes []Node) evaluation.ClassificationGate {
	gate := evaluation.ClassificationGate{Status: "pass", PolicySHA256: set.PolicyHash, TaxonomyVersion: set.Taxonomy}
	for _, node := range nodes {
		if node.Origin == "custom" {
			gate.Status = "unavailable"
			gate.Reasons = append(gate.Reasons, "custom node reference slice is unavailable")
			break
		}
	}
	refs := make(map[string]ClassificationReference, len(set.References))
	for _, ref := range set.References {
		if _, exists := refs[ref.Slice]; exists {
			gate.Status = "invalid"
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("duplicate reference slice %q", ref.Slice))
			continue
		}
		refs[ref.Slice] = ref
	}
	pins := make(map[string]gateLabels, len(labels))
	for _, pin := range labels {
		pins[pin.slice] = pin
	}
	menu := make(map[string]bool, len(set.Policy.Menu))
	for _, family := range set.Policy.Menu {
		menu[family.Code] = true
	}
	for _, row := range classificationFloors {
		pin := pins[row.slice]
		ref, found := refs[row.slice]
		slice := evaluation.ClassificationSlice{Slice: row.slice, LabelsSHA256: pin.hash}
		if !found {
			slice.Status, slice.Reasons = "unavailable", []string{"missing reference slice"}
		} else {
			delete(refs, row.slice)
			scoreClassificationSlice(&slice, pin, ref, menu, articles, nodes, row.floors[:])
		}
		gate.Slices = append(gate.Slices, slice)
		switch slice.Status {
		case "invalid":
			gate.Status = slice.Status
		case "unavailable":
			if gate.Status != "invalid" {
				gate.Status = slice.Status
			}
		case "fail":
			if gate.Status == "pass" {
				gate.Status = slice.Status
			}
		}
	}
	if len(refs) != 0 {
		gate.Status = "invalid"
		gate.Reasons = append(gate.Reasons, "unexpected reference slice")
	}
	return gate
}

func scoreClassificationSlice(slice *evaluation.ClassificationSlice, pin gateLabels, ref ClassificationReference, menu map[string]bool, articles []Article, nodes []Node, floors []gateFloor) {
	slice.Entries = len(pin.labels.Articles)
	if pin.hash == "" || ref.LabelsHash != pin.hash || ref.Provider != pin.provider {
		slice.Status, slice.Reasons = "invalid", []string{"reference label pin or provider differs"}
		return
	}
	family, prediction, err := classificationPredictions(pin.labels, ref, menu)
	if err != nil {
		slice.Status, slice.Reasons = "invalid", []string{err.Error()}
		return
	}
	score, err := Score(pin.labels, family, prediction, articles, nodes)
	slice.InvalidEntries = score.InvalidEntries
	if err != nil && score.Status != "unavailable" {
		slice.Status, slice.Reasons = "invalid", []string{err.Error()}
		return
	}
	slice.ExcludedFragments = make([]evaluation.ClassificationFragment, 0, score.FragmentCount)
	for _, fragment := range score.ExcludedFragments {
		slice.ExcludedFragments = append(slice.ExcludedFragments, evaluation.ClassificationFragment{DocumentKey: fragment.DocumentKey, Anchor: fragment.Anchor})
	}
	level := score.Levels[0]
	slice.Level2 = &evaluation.ClassificationLevel2{
		All:         classificationCounts(level.All.Raw, score.FamilyHits, score.FamilyTotal),
		NonFragment: classificationCounts(level.NonFragment.Raw, score.NonFragmentFamilyHits, score.NonFragmentFamilyTotal),
	}
	if err != nil {
		slice.Status, slice.Reasons = "unavailable", []string{err.Error()}
		return
	}
	slice.Status = "pass"
	for _, floor := range floors {
		count := slice.Level2.NonFragment[floor.metric]
		if count.Total == 0 || 100*count.Hits < floor.percent*count.Total {
			slice.Status = "fail"
			slice.Reasons = append(slice.Reasons, fmt.Sprintf("%s: %d/%d does not meet %d%% with a nonzero denominator", floor.metric, count.Hits, count.Total, floor.percent))
		}
	}
}

func classificationCounts(m Metrics, familyHits, familyTotal int) map[string]evaluation.ClassificationCount {
	return map[string]evaluation.ClassificationCount{
		"recall":          {Hits: m.TruePositive, Total: m.TruePositive + m.FalseNegative},
		"precision":       {Hits: m.TruePositive, Total: m.TruePositive + m.FalsePositive},
		"top_1_precision": {Hits: m.topOneHits, Total: m.topOneTotal},
		"family_recall":   {Hits: familyHits, Total: familyTotal},
	}
}

func classificationPredictions(labels taxonomy.Labels, ref ClassificationReference, menu map[string]bool) (FamilyArtifact, ArticleArtifact, error) {
	byKey := make(map[packArticleKey]string, len(labels.Articles))
	docKeys := map[string]bool{}
	for _, label := range labels.Articles {
		docKeys[label.Key] = true
		byKey[packArticleKey{label.Key, label.Anchor}] = label.OwnTextSHA256
	}
	families := FamilyArtifact{Documents: make(map[string]Outcome, len(ref.Documents))}
	if len(ref.Documents) != len(docKeys) || len(ref.Articles) != len(byKey) {
		return families, ArticleArtifact{}, fmt.Errorf("incomplete document or article predictions")
	}
	for _, doc := range ref.Documents {
		if !docKeys[doc.Key] || !validKey(doc.RequestHash) {
			return families, ArticleArtifact{}, fmt.Errorf("invalid document prediction for %q", doc.Key)
		}
		delete(docKeys, doc.Key)
		switch doc.Outcome {
		case "ok", "no_family", "invalid_response":
		default:
			return families, ArticleArtifact{}, fmt.Errorf("unknown document outcome for %q", doc.Key)
		}
		if (doc.Outcome == "ok") != (len(doc.Codes) > 0) {
			return families, ArticleArtifact{}, fmt.Errorf("inconsistent families for %q", doc.Key)
		}
		seen := map[string]bool{}
		for _, code := range doc.Codes {
			if !menu[code] || seen[code] {
				return families, ArticleArtifact{}, fmt.Errorf("invalid family code for %q", doc.Key)
			}
			seen[code] = true
		}
		families.Documents[doc.Key] = Outcome{Outcome: doc.Outcome, Codes: doc.Codes}
	}
	var prediction ArticleArtifact
	for _, article := range ref.Articles {
		key := packArticleKey{article.Key, article.Anchor}
		if sha, found := byKey[key]; !found || sha != article.OwnTextSHA256 {
			return families, prediction, fmt.Errorf("invalid article prediction triple for %q %q", key.document, key.anchor)
		}
		delete(byKey, key)
		out := ArticleOutcome{Outcome: article.Outcome, Codes: article.Codes}
		doc := families.Documents[article.Key]
		if err := validateArticleOutcome(out); err != nil || (doc.Outcome != "ok" && doc.Outcome != out.Outcome) {
			return families, prediction, fmt.Errorf("invalid article outcome for %q %q", key.document, key.anchor)
		}
		prediction.Articles = append(prediction.Articles, ArticleRecord{key.document, key.anchor, out})
	}
	return families, prediction, nil
}
