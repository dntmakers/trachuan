package classify

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func TestClassificationReferences(t *testing.T) {
	for _, tc := range []struct {
		name, status, reason string
		change               func(*ClassificationSet, []Article, *[]Node)
	}{
		{"all slices pass at confidence zero", "pass", "", nil},
		{"seeded recall below floor", "fail", "recall", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			for i := range s.References[0].Articles[:4] {
				s.References[0].Articles[i].Codes = nil
			}
		}},
		{"exact recall floor passes", "pass", "", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			for i := range s.References[0].Articles[:3] {
				s.References[0].Articles[i].Codes = nil
			}
		}},
		{"missing reference", "unavailable", "missing", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			s.References = s.References[:2]
		}},
		{"more than five percent invalid", "unavailable", "more than 5 percent", func(_ *ClassificationSet, a []Article, _ *[]Node) {
			a[0].OwnText, a[1].OwnText = "changed first", "changed second"
		}},
		{"incorrect reference pin", "invalid", "pin", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			s.References[0].LabelsHash = strings.Repeat("d", 64)
		}},
		{"missing prediction", "invalid", "prediction", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			s.References[0].Articles = s.References[0].Articles[:19]
		}},
		{"altered prediction triple", "invalid", "triple", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			s.References[0].Articles[0].OwnTextSHA256 = strings.Repeat("d", 64)
		}},
		{"zero precision denominator", "fail", "top_1_precision", func(s *ClassificationSet, _ []Article, _ *[]Node) {
			for i := range s.References[0].Articles {
				s.References[0].Articles[i].Codes = nil
			}
		}},
		{"custom nodes require another slice", "unavailable", "custom", func(_ *ClassificationSet, _ []Article, nodes *[]Node) {
			*nodes = append(*nodes, Node{Code: "1.2", Level: 2, Active: true, Origin: "custom"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, labels, articles, nodes := gateScoreFixture(t)
			if tc.change != nil {
				tc.change(&set, articles, &nodes)
			}
			gate := scoreClassificationReferences(set, labels, articles, nodes)
			if gate.Status != tc.status {
				t.Fatalf("gate status = %q, want %q: %+v", gate.Status, tc.status, gate)
			}
			var reasons []string
			reasons = append(reasons, gate.Reasons...)
			for _, slice := range gate.Slices {
				reasons = append(reasons, slice.Reasons...)
			}
			if !strings.Contains(strings.Join(reasons, "; "), tc.reason) || len(gate.Slices) != 3 {
				t.Fatalf("gate reasons = %q, want %q across three slices: %+v", reasons, tc.reason, gate)
			}
		})
	}
}

func TestClassificationGateUsesSliceSpecificPrecision(t *testing.T) {
	for _, tc := range []struct {
		slice int
		wrong int
		want  string
	}{
		{0, 4, "pass"},
		{0, 5, "fail"},
		{1, 8, "pass"},
		{1, 9, "fail"},
		{2, 8, "pass"},
		{2, 9, "fail"},
	} {
		set, labels, articles, nodes := gateScoreFixture(t)
		set.Policy.Menu = append(set.Policy.Menu, Family{Code: "2.1"})
		ref := &set.References[tc.slice]
		ref.Documents[0].Codes = append(ref.Documents[0].Codes, "2.1")
		for i := range tc.wrong {
			ref.Articles[i].Codes = append(ref.Articles[i].Codes, CodeConfidence{"2.1", 1})
		}
		gate := scoreClassificationReferences(set, labels, articles, nodes)
		if gate.Status != tc.want || gate.Slices[tc.slice].Status != tc.want {
			t.Fatalf("%s with %d false positives: status = %q, want %q; %+v", ref.Slice, tc.wrong, gate.Status, tc.want, gate.Slices[tc.slice])
		}
	}
}

func TestClassificationGateRequiresFamilyRecall(t *testing.T) {
	set, labels, articles, nodes := gateScoreFixture(t)
	for i := range 3 {
		labels[0].labels.Articles[i].Codes = []string{"2.1"}
	}
	gate := scoreClassificationReferences(set, labels, articles, nodes)
	slice := gate.Slices[0]
	if gate.Status != "fail" || len(slice.Reasons) != 1 || !strings.HasPrefix(slice.Reasons[0], "family_recall: 17/20") {
		t.Fatalf("family recall must fail despite passing article metrics: %+v", slice)
	}
}

func TestClassificationGateUsesNonFragmentRawFloors(t *testing.T) {
	set, labels, articles, nodes := gateScoreFixture(t)
	articles[0].OwnText = "Điều 1. Heading\nThông tư này có hiệu lực từ ngày 1 tháng 1 năm 2027."
	fragmentHash := taxonomy.OwnTextSHA256(articles[0].OwnText)
	for i := range set.References {
		labels[i].labels.Articles[0].OwnTextSHA256 = fragmentHash
		set.References[i].Articles[0].OwnTextSHA256 = fragmentHash
	}
	articles[1].OwnText = articles[0].OwnText
	set.References[0].Articles[0].Codes = nil
	gate := scoreClassificationReferences(set, labels, articles, nodes)
	slice := gate.Slices[0]
	if gate.Status != "pass" || slice.InvalidEntries != 1 || len(slice.ExcludedFragments) != 1 || slice.ExcludedFragments[0].Anchor != articles[0].Anchor {
		t.Fatalf("unexpected fragment handling: %+v", slice)
	}
	if got := slice.Level2.NonFragment["recall"]; got != (evaluation.ClassificationCount{Hits: 18, Total: 19}) {
		t.Fatalf("recall = %+v, want 18/19", got)
	}
	if got := slice.Level2.NonFragment["family_recall"]; got != (evaluation.ClassificationCount{Hits: 18, Total: 19}) {
		t.Fatalf("family recall = %+v, want 18/19", got)
	}
}

func gateScoreFixture(t *testing.T) (ClassificationSet, []gateLabels, []Article, []Node) {
	t.Helper()
	labels, _, _, articles, nodes := scoreFixture(t, 20)
	nodes = slices.DeleteFunc(nodes, func(node Node) bool { return node.Origin == "custom" })
	set := ClassificationSet{Policy: ClassificationPolicy{Menu: []Family{{Code: "1.1"}}}}
	var pins []gateLabels
	for i, name := range []string{"seeded", "multi-family", "post-2023"} {
		provider := "anthropic"
		if i == 0 {
			provider = "package"
		}
		pin := fmt.Sprintf("%064d", i+1)
		ref := ClassificationReference{Slice: name, Provider: provider, LabelsHash: pin, Documents: []PackedDocument{{Key: articles[0].DocumentKey, RequestHash: strings.Repeat("a", 64), Outcome: "ok", Codes: []string{"1.1"}}}}
		for _, label := range labels.Articles {
			ref.Articles = append(ref.Articles, PackedArticle{Key: label.Key, Anchor: label.Anchor, OwnTextSHA256: label.OwnTextSHA256, Outcome: "ok", Codes: []CodeConfidence{{"1.1.c1.m1", 0}}})
		}
		set.References = append(set.References, ref)
		sliceLabels := labels
		sliceLabels.Articles = append([]taxonomy.LabelArticle(nil), labels.Articles...)
		pins = append(pins, gateLabels{slice: name, provider: provider, hash: pin, labels: sliceLabels})
	}
	return set, pins, articles, nodes
}
