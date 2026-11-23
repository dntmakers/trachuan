package build

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

const classificationKey = "vn:vbpl:1"
const classificationText = "Substantive legal text"

func matchNodes() map[string]taxonomyNode {
	return map[string]taxonomyNode{
		"1.1":    {id: 1, family: 1, level: 2, status: "active"},
		"1.2":    {id: 2, family: 2, level: 2, status: "active"},
		"1.1.c1": {id: 3, family: 1, level: 3, status: "active"},
		"1.1.c2": {id: 4, family: 1, level: 3, status: "retired"},
	}
}

func matchSet() classify.ClassificationSet {
	return classify.ClassificationSet{
		Policy:    classify.ClassificationPolicy{Menu: []classify.Family{{Code: "1.1"}, {Code: "1.2"}}},
		Documents: []classify.PackedDocument{{Key: classificationKey, RequestHash: strings.Repeat("a", 64), Outcome: "ok", Codes: []string{"1.1", "1.2"}}},
		Articles:  []classify.PackedArticle{{Key: classificationKey, Anchor: "dieu-1", OwnTextSHA256: taxonomy.OwnTextSHA256(classificationText), Outcome: "ok", Codes: []classify.CodeConfidence{{Code: "1.1", Confidence: 0.2}, {Code: "1.1.c1", Confidence: 0.8}, {Code: "1.2", Confidence: 0}}}},
	}
}

func TestClassificationMatcher_UsesOnlyExactTriple(t *testing.T) {
	m, err := newClassificationMatcher(matchSet(), matchNodes())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, key, anchor, text, gap string }{
		{"document", "vn:vbpl:2", "dieu-1", classificationText, "missing_result"},
		{"anchor", classificationKey, "dieu-2", classificationText, "missing_result"},
		{"hash", classificationKey, "dieu-1", "Other legal text", "changed_own_text"},
		{"exact", classificationKey, "dieu-1", classificationText, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, reason := m.match(tc.key, tc.anchor, tc.text)
			if reason != tc.gap {
				t.Fatalf("gap=%q want %q", reason, tc.gap)
			}
			if tc.gap != "" && len(rows) != 0 {
				t.Fatalf("gap and rows: %v", rows)
			}
			if tc.gap == "" && (len(rows) != 2 || rows[1] != 0.8 || rows[2] != 0) {
				t.Fatalf("rows=%v", rows)
			}
		})
	}
}

func TestClassificationMatcher_RejectsInvalidUnusedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, code    string
		confidence    float64
		documentCodes []string
	}{
		{"unknown", "1.1.unknown", 0.5, []string{"1.1", "1.2"}},
		{"inactive", "1.1.c2", 0.5, []string{"1.1", "1.2"}},
		{"confidence", "1.1", 1.1, []string{"1.1", "1.2"}},
		{"family", "1.2", 0.5, []string{"1.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := matchSet()
			set.Documents[0].Codes = tc.documentCodes
			set.Articles[0].Codes = []classify.CodeConfidence{{Code: tc.code, Confidence: tc.confidence}}
			if _, err := newClassificationMatcher(set, matchNodes()); err == nil {
				t.Fatal("accepted invalid evidence before any match")
			}
		})
	}
}

func TestClassificationMatcher_ReportsTerminalOutcome(t *testing.T) {
	for _, tc := range []struct{ document, article, gap string }{
		{"no_family", "no_family", "no_family"},
		{"ok", "no_family", "no_family"},
		{"invalid_response", "invalid_response", "invalid_response"},
		{"ok", "invalid_response", "invalid_response"},
		{"ok", "too_long", "too_long"},
		{"ok", "ok", "no_code"},
	} {
		t.Run(tc.document+"/"+tc.article, func(t *testing.T) {
			set := matchSet()
			set.Documents[0].Outcome = tc.document
			if tc.document != "ok" {
				set.Documents[0].Codes = nil
			}
			set.Articles[0].Outcome, set.Articles[0].Codes = tc.article, nil
			m, err := newClassificationMatcher(set, matchNodes())
			if err != nil {
				t.Fatal(err)
			}
			rows, detail := m.match(classificationKey, "dieu-1", classificationText)
			if len(rows) != 0 || detail != tc.gap {
				t.Fatalf("rows=%v gap=%q want %q", rows, detail, tc.gap)
			}
		})
	}
}
