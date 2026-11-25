package retrieval

import (
	"strings"
	"testing"
)

func TestSnippetBodyCutOverlappingChild(t *testing.T) {
	parentID := int64(1)
	parent := Identity{Country: "vn", DocumentKey: "vn:vbpl:179252", Anchor: "art_1"}
	child := Identity{Country: "vn", DocumentKey: parent.DocumentKey, Anchor: "art_1/cl_1"}
	childText := strings.Repeat("child text ", 20)
	tree := renderTree([]Provision{{ID: 1, Identity: parent, Text: "own words " + childText}, {ID: 2, ParentID: &parentID, Identity: child, Text: childText}}, 1)
	h := Hit{Identity: parent, Text: string(tree.text), Fragments: tree.fragments}
	_, _, cut := SnippetWithBodyCut(h, 30)
	if cut {
		t.Fatal("child-only omission counted as parent body cut")
	}
}

func TestSnippetBodyCut(t *testing.T) {
	id := Identity{DocumentKey: "vn:vbpl:179252", Anchor: "art_1"}
	child := Identity{DocumentKey: id.DocumentKey, Anchor: "art_1/cl_1"}
	tests := []struct {
		name, text string
		fragments  []Fragment
		best, cap  int
		want       bool
	}{
		{"named body cut", "alpha beta gamma delta", []Fragment{{Identity: id, Role: "body", Start: 0, End: 22, TextStart: 0, TextEnd: 22}}, 0, 12, true},
		{"lead cut only", strings.Repeat("lead ", 20) + "body", []Fragment{{Identity: child, Role: "lead", Start: 0, End: 100, TextStart: 0, TextEnd: 100}, {Identity: id, Role: "body", Start: 0, End: 4, TextStart: 100, TextEnd: 104}}, 100, 20, false},
		{"child cut only", "own " + strings.Repeat("child ", 20), []Fragment{{Identity: id, Role: "body", Start: 0, End: 4, TextStart: 0, TextEnd: 4}, {Identity: child, Role: "body", Start: 0, End: 120, TextStart: 4, TextEnd: 124}}, 0, 20, false},
		{"no fragments", "alpha beta gamma delta", nil, 0, 12, false},
		{"retained duplicate source", "alpha beta alpha beta", []Fragment{{Identity: id, Role: "body", Start: 0, End: 10, TextStart: 0, TextEnd: 10}, {Identity: id, Role: "body", Start: 0, End: 10, TextStart: 11, TextEnd: 21}}, 0, 15, false},
		{"retained copy in lead", "alpha beta\nalpha beta" + strings.Repeat(" child", 10), []Fragment{{Identity: id, Role: "lead", Start: 0, End: 10, TextStart: 0, TextEnd: 10}, {Identity: id, Role: "body", Start: 0, End: 10, TextStart: 11, TextEnd: 21}}, 11, 22, false},
		{"whitespace omitted", "alpha           ", []Fragment{{Identity: id, Role: "body", Start: 0, End: 16, TextStart: 0, TextEnd: 16}}, 0, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Hit{Identity: id, Text: tt.text, Fragments: tt.fragments, BestTextStart: tt.best}
			_, _, got := SnippetWithBodyCut(h, tt.cap)
			if got != tt.want {
				t.Fatalf("cut = %v, want %v", got, tt.want)
			}
		})
	}
}
