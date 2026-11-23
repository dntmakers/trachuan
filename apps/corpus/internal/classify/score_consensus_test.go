package classify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func consensusPath(slice string) string {
	return filepath.Join("..", "evaluation", "testdata", "vn", "taxonomy-"+slice+"-consensus-v1.json")
}

func TestScoreConsensusPins(t *testing.T) {
	for _, slice := range []string{"multi-family", "post-2023"} {
		data, err := os.ReadFile(consensusPath(slice))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := VerifyScoreLabels(Recipe{Provider: "openai", Model: "gpt-6.2-sol"}, data); err != nil || got != slice {
			t.Fatalf("Sol: slice %q, error %v", got, err)
		}
		for _, recipe := range []Recipe{{Provider: "openai", Model: "gpt-6.2-astra"}, {Provider: "openai", Model: "gpt-6.1-terra"}, {Provider: "anthropic", Model: "claude-haiku-5-5"}} {
			if _, err := VerifyScoreLabels(recipe, data); err == nil {
				t.Fatalf("accepted %s/%s", recipe.Provider, recipe.Model)
			}
		}
		path := filepath.Join(t.TempDir(), "edited.json")
		data = append(data, ' ')
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyScoreLabels(Recipe{Provider: "openai", Model: "gpt-6.2-sol"}, data); err == nil {
			t.Fatal("accepted changed score pin")
		}
		if _, _, err := ReadLabels(path); err == nil {
			t.Fatal("accepted consensus forms without an exact pin")
		}
	}
}

func consensusScoreFixture(t *testing.T, n int) (taxonomy.Labels, FamilyArtifact, ArticleArtifact, []Article, []Node) {
	t.Helper()
	base, f, a, release, nodes := scoreFixture(t, n)
	l, _, err := ReadLabels(consensusPath("post-2023"))
	if err != nil {
		t.Fatal(err)
	}
	l.Articles = base.Articles
	return l, f, a, release, nodes
}

func TestScoreConsensus_MixedDepth(t *testing.T) {
	l, f, a, release, nodes := consensusScoreFixture(t, 1)
	nodes = append(nodes, Node{Code: "2.1", Level: 2, Active: true, Origin: "phapdien"}, Node{Code: "2.1.c1", Parent: "2.1", Level: 3, Active: true, Origin: "phapdien"})
	l.Articles[0].Codes = []string{"1.1.c1.m1", "1.1.c1", "2.1"}
	l.Articles[0].Level2OnlyCodes = []string{"2.1"}
	f.Documents[release[0].DocumentKey] = Outcome{Outcome: "ok", Codes: []string{"1.1", "2.1"}}
	a.Articles[0].Outcome.Codes = []CodeConfidence{{"2.1.c1", 0.99}, {"1.1.c1.m1", 0.8}, {"1.1.c1", 0.6}}
	r, err := Score(l, f, a, release, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if r.NonFragmentFamilyHits != 3 || r.NonFragmentFamilyTotal != 3 || r.Levels[0].NonFragment.Raw.TruePositive != 2 {
		t.Fatalf("level 2: %+v", r)
	}
	for _, level := range r.Levels[1:] {
		m := level.NonFragment.Raw
		if m.topOneHits != 1 || m.topOneTotal != 1 || m.FalsePositive != 0 || m.AdditionalTotal != m.AdditionalCorrect || len(level.NonFragment.Candidates) != 3 {
			t.Fatalf("deep level: %+v", level)
		}
	}
}

func TestScoreConsensus_EmptyFragmentAndInvalidBoundary(t *testing.T) {
	for _, invalid := range []int{0, 5, 6} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			l, f, a, release, nodes := consensusScoreFixture(t, 100)
			for i := range 3 {
				l.Articles[i].Codes, l.Articles[i].ConfirmedEmpty = []string{}, true
			}
			a.Articles[0].Outcome.Codes = append(a.Articles[0].Outcome.Codes, CodeConfidence{"1.1.c1", 0.2})
			a.Articles[2].Outcome.Codes = nil
			const fragment = "Điều 1. Heading\nThông tư này có hiệu lực từ ngày 1 tháng 1 năm 2027."
			release[1].OwnText = fragment
			l.Articles[1].OwnTextSHA256 = taxonomy.OwnTextSHA256(fragment)
			if invalid > 0 {
				release[0].OwnText = fragment
				for i := 3; i < invalid+2; i++ {
					release[i].OwnText = fragment
				}
			}
			r, err := Score(l, f, a, release, nodes)
			if invalid == 6 {
				if err == nil || !strings.Contains(err.Error(), "more than 5 percent") || r.Status != "unavailable" {
					t.Fatalf("six invalid labels: %+v, %v", r, err)
				}
			} else if err != nil || r.Status != "available" {
				t.Fatalf("%d invalid labels: %+v, %v", invalid, r, err)
			}
			if r.InvalidEntries != invalid || r.FragmentCount != 1 || r.NonFragmentEntries != 99 || r.NonFragmentFamilyTotal != 97 {
				t.Fatalf("reference counts: %+v", r)
			}
			for _, level := range r.Levels {
				m := level.NonFragment.Raw
				if level.All.Raw.FalsePositive != 3 || m.FalsePositive != 2 || m.topOneTotal != m.TruePositive+1 || m.TruePositive+m.FalseNegative != 97 {
					t.Fatalf("empty article counts for %s: %+v", level.Level, level)
				}
			}
		})
	}
}

func TestScoreConsensus_RejectsMalformedMarkers(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*taxonomy.LabelArticle)
	}{
		{"duplicate", func(a *taxonomy.LabelArticle) { a.Codes, a.Level2OnlyCodes = []string{"1.1"}, []string{"1.1", "1.1"} }},
		{"not in codes", func(a *taxonomy.LabelArticle) { a.Level2OnlyCodes = []string{"1.1"} }},
		{"not level two", func(a *taxonomy.LabelArticle) { a.Level2OnlyCodes = a.Codes }},
		{"missing empty marker", func(a *taxonomy.LabelArticle) { a.Codes = []string{} }},
		{"null codes", func(a *taxonomy.LabelArticle) { a.Codes, a.ConfirmedEmpty = nil, true }},
		{"marked nonempty", func(a *taxonomy.LabelArticle) { a.ConfirmedEmpty = true }},
		{"empty no node", func(a *taxonomy.LabelArticle) { a.Codes, a.NoNode, a.ConfirmedEmpty = []string{}, true, true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, f, a, release, nodes := consensusScoreFixture(t, 1)
			tc.edit(&l.Articles[0])
			if _, err := Score(l, f, a, release, nodes); err == nil {
				t.Fatal("accepted malformed markers")
			}
		})
	}
}
