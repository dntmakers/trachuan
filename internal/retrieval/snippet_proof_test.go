package retrieval

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSnippetBodyCutSubtree(t *testing.T) {
	parentID, clauseID := int64(1), int64(2)
	parent := Identity{DocumentKey: "vn:vbpl:179252", Anchor: "art_1"}
	clause := Identity{DocumentKey: parent.DocumentKey, Anchor: "clause"}
	point := Identity{DocumentKey: parent.DocumentKey, Anchor: "point"}
	tests := []struct {
		name      string
		nodes     []Provision
		cap, best int
		lead      string
		want      bool
	}{
		{"point cut", []Provision{{ID: 1, Identity: parent}, {ID: 2, ParentID: &parentID, Identity: clause, Text: "clause lead"}, {ID: 3, ParentID: &clauseID, Identity: point, Text: strings.Repeat("point text ", 20)}}, 50, 0, "", true},
		{"child heading only", []Provision{{ID: 1, Identity: parent, Text: "Điều 2. Heading\nrule text"}, {ID: 2, ParentID: &parentID, Identity: clause, Label: "Điều 2", Heading: "Heading", Text: "Điều 2. Heading\nrule text"}}, 20, 16, "", false},
		{"heading only", []Provision{{ID: 1, Identity: parent, Label: "Điều 1", Heading: "Heading", Text: "Điều 1. Heading\nrule text"}}, 20, 16, "", false},
		{"whitespace only", []Provision{{ID: 1, Identity: parent, Text: "alpha" + strings.Repeat(" ", 30) + "beta"}}, 20, 35, "", false},
		{"named rule cut", []Provision{{ID: 1, Identity: parent, Text: strings.Repeat("rule text ", 20)}}, 30, 0, "", true},
		{"ancestor lead cut", []Provision{{ID: 1, Identity: parent, Text: "whole body"}}, 30, 0, strings.Repeat("lead ", 30), false},
		{"unknown heading", []Provision{{ID: 1, Identity: parent, Label: "Điều 1", Heading: "Heading", Text: strings.Repeat("rule text ", 20)}}, 30, 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := renderTree(tt.nodes, 1)
			h := Hit{Identity: parent, Text: string(tree.text), Fragments: tree.fragments, rulePrefixes: rulePrefixes(tt.nodes), ruleLeaves: ruleLeaves(tt.nodes), BestTextStart: tt.best}
			if tt.name == "whitespace only" {
				h.Fragments = append(h.Fragments, Fragment{Identity: parent, Role: "lead", Start: 0, End: 5, TextStart: 0, TextEnd: 5})
			}
			if tt.lead != "" {
				lead := []rune(tt.lead)
				h.Text = tt.lead + "\n" + h.Text
				for i := range h.Fragments {
					h.Fragments[i].TextStart += len(lead) + 1
					h.Fragments[i].TextEnd += len(lead) + 1
				}
				h.Fragments = append(h.Fragments, Fragment{Identity: clause, Role: "lead", Start: 0, End: len(lead), TextStart: 0, TextEnd: len(lead)})
			}
			_, _, got := SnippetWithBodyCut(h, tt.cap)
			if got != tt.want {
				t.Fatalf("cut = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSnippetBodyCutParentCopy(t *testing.T) {
	parentID := int64(1)
	parent, child := Identity{Anchor: "parent"}, Identity{Anchor: "child"}
	kept := strings.Repeat("a", 30)
	copied := strings.Repeat("b", 30)
	body := kept + " " + copied
	nodes := []Provision{{ID: 1, Identity: parent, Text: body}, {ID: 2, ParentID: &parentID, Identity: child, Text: body}}
	tree := renderTree(nodes, 1)
	shift := len([]rune(copied)) + 1
	h := Hit{Identity: parent, Text: copied + "\n" + string(tree.text), rulePrefixes: rulePrefixes(nodes), ruleLeaves: ruleLeaves(nodes)}
	h.Fragments = []Fragment{{Identity: parent, Role: "lead", Start: len([]rune(kept)) + 1, End: len([]rune(body)), TextStart: 0, TextEnd: len([]rune(copied))}}
	for i := len(tree.fragments) - 1; i >= 0; i-- {
		f := tree.fragments[i]
		f.TextStart += shift
		f.TextEnd += shift
		h.Fragments = append(h.Fragments, f)
	}
	snippet, _, cut := SnippetWithBodyCut(h, 68)
	if !strings.Contains(snippet, kept) || !strings.Contains(snippet, copied) {
		t.Fatalf("snippet lacks retained source copies: %q", snippet)
	}
	if cut {
		t.Fatal("retained parent copy must retain child coordinates")
	}
}

func TestShapedHitMinimumRuleRuns(t *testing.T) {
	parentID := int64(2)
	heading := "A heading repeated entirely outside the child"
	child := "Khoản 1. " + heading + "\n" + strings.Repeat("retained rule ", 4)
	tests := []struct {
		name      string
		text      string
		label     string
		children  []Provision
		best, cap int
		want      bool
	}{
		{"repeated heading", child + "\n" + heading, "", []Provision{{ID: 3, NodeID: "child", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "child"}, Label: "Khoản 1", Heading: heading, Text: child}}, 0, len([]rune(child)) + 4, false},
		{"repeated twice", child + "\n" + heading + "\n" + heading, "", []Provision{{ID: 3, NodeID: "child", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "child"}, Label: "Khoản 1", Heading: heading, Text: child}}, 0, len([]rune(child)) + 4, false},
		{"partial repeat", child + "\n" + heading, "", []Provision{{ID: 3, NodeID: "child", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "child"}, Label: "Khoản 1", Heading: heading, Text: child}}, 0, len([]rune(child)) + 27, false},
		{"spaced delimiter", "Điều 19 ." + strings.Repeat(" ", 30) + strings.Repeat("retained rule ", 3), "Điều 19", []Provision{{ID: 3, NodeID: "child", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "child"}, Text: strings.Repeat("retained rule ", 3)}}, 39, 46, false},
		{"19 kept", strings.Repeat("a", 19) + " " + strings.Repeat("b", 40), "", nil, 0, 23, false},
		{"19 omitted", strings.Repeat("a", 30) + " " + strings.Repeat("b", 19), "", nil, 0, 34, false},
		{"20 omitted", strings.Repeat("a", 30) + " " + strings.Repeat("b", 20), "", nil, 0, 34, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, recipe := shapeFixture(tt.text)
			c := a.Primary.Dense[0]
			c.Provision.Label = tt.label
			c.Ancestors = []Provision{c.Provision}
			c.Descendants = append([]Provision{c.Provision}, tt.children...)
			if tt.best > 0 {
				c.Content = string([]rune(tt.text)[tt.best:])
				c.Spans = []byte(fmt.Sprintf(`[{"node_id":"c","start":%d,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, tt.best, len([]rune(tt.text)), len([]rune(c.Content))))
			}
			a.Primary.Dense[0], a.Primary.Members[0] = c, c
			hits, err := ShapeGroups(a, recipe, 1, "dense")
			if err != nil || len(hits) != 1 {
				t.Fatalf("shape: %v, hits %d", err, len(hits))
			}
			_, _, cut := SnippetWithBodyCut(hits[0], tt.cap)
			if cut != tt.want {
				t.Fatalf("cut = %v, want %v", cut, tt.want)
			}
		})
	}
}

func TestShapedHitParentIntroductionOnly(t *testing.T) {
	parentID := int64(2)
	intro := strings.Repeat("parent introduction ", 5)
	leaf := strings.Repeat("leaf rule text ", 4)
	text := intro + "\n" + leaf
	a, recipe := shapeFixture(text)
	c := a.Primary.Dense[0]
	c.Provision.Label = ""
	c.Ancestors = []Provision{c.Provision}
	c.Descendants = []Provision{c.Provision, {ID: 3, NodeID: "leaf", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "leaf"}, Text: leaf}}
	best := len([]rune(intro)) + 1
	c.Content = leaf
	c.Spans = []byte(fmt.Sprintf(`[{"node_id":"c","start":%d,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, best, len([]rune(text)), len([]rune(leaf))))
	a.Primary.Dense[0], a.Primary.Members[0] = c, c
	hits, err := ShapeGroups(a, recipe, 1, "dense")
	if err != nil || len(hits) != 1 {
		t.Fatalf("shape: %v, hits %d", err, len(hits))
	}
	_, _, cut := SnippetWithBodyCut(hits[0], len([]rune(leaf))+5)
	if cut {
		t.Fatal("parent introduction alone must not prove a leaf cut")
	}
}

func TestShapedHitTruncatedHeadingRepeat(t *testing.T) {
	parentID := int64(2)
	heading := "A long child heading repeated beyond the evidence limit"
	child := "Khoản 1. " + heading + "\n" + strings.Repeat("leaf rule text ", 4)
	text := child + "\n" + heading
	a, recipe := shapeFixture(text)
	c := a.Primary.Dense[0]
	c.Provision.Label = ""
	c.Ancestors = []Provision{c.Provision}
	c.Descendants = []Provision{c.Provision, {ID: 3, NodeID: "leaf", ParentID: &parentID, Identity: Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "leaf"}, Label: "Khoản 1", Heading: heading, Text: child}}
	c.Content = child + "\n" + string([]rune(heading)[:35])
	size := len([]rune(c.Content))
	c.Spans = []byte(fmt.Sprintf(`[{"node_id":"c","start":0,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, size, size))
	recipe.EvidenceRuneCap = size
	a.Primary.Dense[0], a.Primary.Members[0] = c, c
	hits, err := ShapeGroups(a, recipe, 1, "dense")
	if err != nil || len(hits) != 1 {
		t.Fatalf("shape: %v, hits %d", err, len(hits))
	}
	if hits[0].Complete || hits[0].Text != c.Content {
		t.Fatalf("unexpected evidence %q", hits[0].Text)
	}
	_, _, cut := SnippetWithBodyCut(hits[0], len([]rune(child))+4)
	if cut {
		t.Fatal("truncated parent heading repeat must not prove leaf cut")
	}
}

func TestShapedHitSourceEnumerators(t *testing.T) {
	articleID, clauseID := int64(1), int64(2)
	id := func(anchor string) Identity { return Identity{Country: "vn", DocumentKey: "vn:1", Anchor: anchor} }
	pointA := "a) " + strings.Repeat("trường hợp cần bảo vệ ", 3)
	pointB := "b) Để giải quyết tình trạng khẩn cấp;"
	pointC := "c) Vì lợi ích công cộng;"
	pointD := "d) " + strings.Repeat("bảo vệ dữ liệu cá nhân ", 8)
	pointDD := "đ) Trường hợp khác theo quy định của pháp luật."
	clauseText := "1. Các trường hợp không cần sự đồng ý:\n" + pointA + "\n" + pointB + "\n" + pointC + "\n" + pointD + "\n" + pointDD
	heading := "Xử lý dữ liệu cá nhân không cần sự đồng ý"
	articleText := "Điều 19. " + heading + "\n" + clauseText
	root := Provision{ID: articleID, NodeID: "a", Kind: "dieu", Identity: id("dieu-19"), Label: "Điều 19", Heading: heading, Text: articleText}
	children := []Provision{
		{ID: clauseID, ParentID: &articleID, NodeID: "c", Kind: "khoan", Identity: id("dieu-19/khoan-1"), Label: "Khoản 1", Text: clauseText},
		{ID: 7, ParentID: &clauseID, NodeID: "a2", Kind: "diem", Identity: id("a"), Label: "Điểm a", Text: pointA, Ordinal: 1},
		{ID: 3, ParentID: &clauseID, NodeID: "b", Kind: "diem", Identity: id("b"), Label: "Điểm b", Text: pointB, Ordinal: 2},
		{ID: 4, ParentID: &clauseID, NodeID: "c2", Kind: "diem", Identity: id("c"), Label: "Điểm c", Text: pointC, Ordinal: 3},
		{ID: 5, ParentID: &clauseID, NodeID: "d", Kind: "diem", Identity: id("d"), Label: "Điểm d", Text: pointD, Ordinal: 4},
		{ID: 6, ParentID: &clauseID, NodeID: "dd", Kind: "diem", Identity: id("đ"), Label: "Điểm đ", Text: pointDD, Ordinal: 5},
	}
	a, recipe := shapeFixture(articleText)
	c := a.Primary.Dense[0]
	c.GroupID, c.ProvisionID, c.Identity, c.Group = articleID, articleID, root.Identity, root.Identity
	c.Provision, c.Ancestors, c.Descendants = root, []Provision{root}, append([]Provision{root}, children...)
	best := len([]rune("Điều 19. " + heading + "\n1. Các trường hợp không cần sự đồng ý:\n" + pointA + "\n"))
	c.Content = string([]rune(articleText)[best:])
	c.Spans = []byte(fmt.Sprintf(`[{"node_id":"a","start":%d,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, best, len([]rune(articleText)), len([]rune(c.Content))))
	a.Primary.Dense[0], a.Primary.Members[0] = c, c
	hits, err := ShapeGroups(a, recipe, 1, "dense")
	if err != nil || len(hits) != 1 {
		t.Fatalf("shape: %v, hits %d", err, len(hits))
	}
	capRunes := len([]rune(pointB+"\n"+pointC+"\n"+"d) bảo vệ dữ liệu cá nhân ")) + 8
	snippet, _, cut := SnippetWithBodyCut(hits[0], capRunes)
	if strings.Contains(snippet, pointA) || !strings.Contains(snippet, pointB) || !strings.Contains(snippet, pointC) || !strings.Contains(snippet, "d) bảo vệ") {
		t.Fatalf("snippet does not reach point d: %q", snippet)
	}
	if !cut {
		t.Fatal("article cut within point d must be proven")
	}
}

func TestShapedHitEnumeratorOnlyOmitted(t *testing.T) {
	for _, tt := range []struct{ label, printed string }{{"Khoản 1", "1."}, {"Điểm b", "b)"}} {
		t.Run(tt.printed, func(t *testing.T) {
			text := tt.printed + strings.Repeat(" ", 30) + "retained rule text"
			a, recipe := shapeFixture(text)
			c := a.Primary.Dense[0]
			c.Provision.Label = tt.label
			c.Ancestors = []Provision{c.Provision}
			c.Descendants = []Provision{c.Provision}
			best := len([]rune(tt.printed)) + 30
			c.Content = "retained rule text"
			c.Spans = []byte(fmt.Sprintf(`[{"node_id":"c","start":%d,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, best, len([]rune(text)), len([]rune(c.Content))))
			a.Primary.Dense[0], a.Primary.Members[0] = c, c
			hits, err := ShapeGroups(a, recipe, 1, "dense")
			if err != nil || len(hits) != 1 {
				t.Fatalf("shape: %v, hits %d", err, len(hits))
			}
			snippet, _, cut := SnippetWithBodyCut(hits[0], 25)
			if !strings.Contains(snippet, c.Content) || cut {
				t.Fatalf("snippet %q, cut %v", snippet, cut)
			}
		})
	}
}

func TestPrintedLabelNormalization(t *testing.T) {
	for _, tt := range []struct {
		label, source string
		want          bool
	}{
		{"Điều 19", "ĐIỀU 19. Nội dung", true},
		{"Khoản 1", "1) Nội dung", true},
		{"Điểm đ", "Đ) Nội dung", true},
		{"Điểm b", "b Nội dung", false},
		{"Điều 19", "Điều 190. Nội dung", false},
		{"Khoản 1", "1x Nội dung", false},
	} {
		t.Run(tt.source, func(t *testing.T) {
			_, got := printedLabel([]rune(tt.source), tt.label)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShapedHitRejectsUnprovenCuts(t *testing.T) {
	id := Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "clause"}
	childID := Identity{Country: "vn", DocumentKey: "vn:1", Anchor: "child"}
	parentID := int64(2)
	childText := "1) Heading\nrule text"
	tests := []struct {
		name      string
		root      Provision
		children  []Provision
		best, cap int
	}{
		{"empty heading label", Provision{ID: 2, NodeID: "c", Identity: id, Label: "Điều 1", Text: "Điều 1. rule text"}, nil, 8, 14},
		{"unknown child prefix", Provision{ID: 2, NodeID: "c", Identity: id, Text: "x) Heading\nrule text"}, []Provision{{ID: 3, ParentID: &parentID, NodeID: "d", Identity: childID, Label: "Khoản 1", Heading: "Heading", Text: "x) Heading\nrule text"}}, 11, 15},
		{"ambiguous child", Provision{ID: 2, NodeID: "c", Identity: id, Text: childText + "\n" + childText}, []Provision{{ID: 3, ParentID: &parentID, NodeID: "d", Identity: childID, Label: "", Heading: "", Text: childText}}, 11, 34},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, recipe := shapeFixture(tt.root.Text)
			c := a.Primary.Dense[0]
			c.Provision = tt.root
			c.Ancestors = []Provision{tt.root}
			c.Descendants = append([]Provision{tt.root}, tt.children...)
			c.Content = string([]rune(tt.root.Text)[tt.best:])
			c.Spans = []byte(fmt.Sprintf(`[{"node_id":"c","start":%d,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, tt.best, len([]rune(tt.root.Text)), len([]rune(c.Content))))
			a.Primary.Dense[0] = c
			a.Primary.Members[0] = c
			hits, err := ShapeGroups(a, recipe, 1, "dense")
			if err != nil || len(hits) != 1 {
				t.Fatalf("shape: %v, hits %d", err, len(hits))
			}
			if hits[0].BestTextStart != tt.best {
				t.Fatalf("best start = %d, want %d", hits[0].BestTextStart, tt.best)
			}
			_, _, cut := SnippetWithBodyCut(hits[0], tt.cap)
			if cut {
				t.Fatal("unverified omission counted as rule text")
			}
		})
	}
}

func TestHitRulePrefixesNotSerialized(t *testing.T) {
	h := Hit{Identity: Identity{Anchor: "a"}}
	before, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	h.rulePrefixes = map[Identity]int{h.Identity: 12}
	h.ruleLeaves = map[Identity]bool{h.Identity: true}
	h.ambiguousTree = true
	after, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rule prefixes changed hit JSON")
	}
}
