package classify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPackReferenceUsesScoringPinAfterReplay(t *testing.T) {
	refs, err := readPackReferences(t.TempDir(), PackBuild{Country: "vn", Release: PackBaselineRelease})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs[1:] {
		if ref.scoring.provider != "consensus" || ref.scoring.hash == ref.selection.hash {
			t.Fatalf("%s: historical and scoring pins were not separated", ref.name)
		}
		var pair packPair
		for _, label := range ref.selection.articles {
			pair.Articles = append(pair.Articles, PackedArticle{Key: label.Key, Anchor: label.Anchor, OwnTextSHA256: label.OwnTextSHA256, Outcome: "no_family"})
		}
		out, err := packScoringReference(ref, pair)
		if err != nil || out.LabelsHash != ref.scoring.hash || out.Provider != "consensus" || len(out.Articles) != 100 {
			t.Fatalf("%s: wrong scoring reference, %v", ref.name, err)
		}
		first := pair.Articles[0]
		pair.Articles[0].OwnTextSHA256 = strings.Repeat("0", 64)
		if _, err := packScoringReference(ref, pair); err == nil {
			t.Fatal("accepted wrong own-text hash on skipped article")
		}
		pair.Articles[0] = pair.Articles[1]
		if _, err := packScoringReference(ref, pair); err == nil {
			t.Fatal("accepted repeated triple in place of skipped article")
		}
		pair.Articles[0] = first
		pair.Articles = pair.Articles[1:]
		if _, err := packScoringReference(ref, pair); err == nil {
			t.Fatal("accepted missing skipped article")
		}
	}
}

func TestClassificationGateRejectsHistoricalPinBeforeReadingRelease(t *testing.T) {
	set := gateConsensusSet(t, "gpt-6.2-sol")
	set.References = []ClassificationReference{{Slice: "multi-family", Provider: "anthropic", LabelsHash: "88e059257a0074d6ea3f59e5fc6e514ccc1e9dc9aaa411e2fb78782774765540"}}
	gate := ScoreClassificationSet(t.Context(), noGateQueries{}, set)
	if gate.Status != "invalid" || !strings.Contains(strings.Join(gate.Reasons, " "), "pin") {
		t.Fatalf("old set: %+v", gate)
	}
}

func TestClassificationGateRejectsJudgeModel(t *testing.T) {
	set := gateConsensusSet(t, "gpt-6.2-astra")
	gate := ScoreClassificationSet(t.Context(), noGateQueries{}, set)
	if gate.Status != "invalid" || !strings.Contains(strings.Join(gate.Reasons, " "), "openai/gpt-6.2-sol") {
		t.Fatalf("judge model: %+v", gate)
	}
}

func TestClassificationGateRejectsJudgeArticlePolicy(t *testing.T) {
	set := gateConsensusSet(t, "gpt-6.2-sol")
	judge := gateConsensusSet(t, "gpt-6.2-astra")
	set.Policy.Article = judge.Policy.Article
	var err error
	set.PolicyHash, err = set.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	gate := ScoreClassificationSet(t.Context(), noGateQueries{}, set)
	if gate.Status != "invalid" || !strings.Contains(strings.Join(gate.Reasons, " "), "article policy") {
		t.Fatalf("judge article policy with Sol document policy: %+v", gate)
	}
}

func TestClassificationGateValidatesPredictionsForStaleReference(t *testing.T) {
	for _, tc := range []struct{ name, code string }{{"unknown node", "unknown"}, {"wrong family", "2.1"}} {
		t.Run(tc.name, func(t *testing.T) {
			set, labels, articles, nodes := gateScoreFixture(t)
			articles[0].OwnText += " changed"
			set.References[1].Articles[0].Codes[0].Code = tc.code
			gate := scoreClassificationReferences(set, labels, articles, nodes)
			if gate.Status != "invalid" || gate.Slices[0].Status != "pass" || gate.Slices[1].Status != "invalid" || gate.Slices[1].InvalidEntries != 1 || gate.Slices[2].Status != "pass" {
				t.Fatalf("invalid prediction on stale triple: gate %q, slice %+v", gate.Status, gate.Slices[1])
			}
		})
	}
}

func TestClassificationGateRejectsBadPredictionInRow(t *testing.T) {
	set, labels, articles, nodes := gateScoreFixture(t)
	set.References[1].Articles[0].Codes[0].Code = "1.1.old"
	gate := scoreClassificationReferences(set, labels, articles, nodes)
	if gate.Status != "invalid" || gate.Slices[0].Status != "pass" || gate.Slices[1].Status != "invalid" || gate.Slices[2].Status != "pass" {
		t.Fatalf("bad prediction: %+v", gate)
	}
}

type noGateQueries struct{}

func (noGateQueries) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("gate tried to read release")
}

func gateConsensusSet(t *testing.T, model string) ClassificationSet {
	t.Helper()
	taxonomy := strings.Repeat("a", 64)
	r, err := NewOpenAIRecipe(model, taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	ar, err := NewArticleRecipe(r, digest(nil))
	if err != nil {
		t.Fatal(err)
	}
	policy, hash, err := packPolicy(r, ar, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ClassificationSet{Version: 1, Source: ClassificationBuild{Country: "vn"}, Taxonomy: taxonomy, Policy: policy, PolicyHash: hash, Baseline: ClassificationBuild{Country: "vn", Release: PackBaselineRelease, InputHash: strings.Repeat("b", 64), RecipeHash: strings.Repeat("c", 64)}}
}
