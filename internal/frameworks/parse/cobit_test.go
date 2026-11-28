package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func cobitWord(x, y, text string) pdftext.Word {
	return pdftext.Word{XMin: x, YMin: y, XMax: x, YMax: y, Text: text}
}
func cobitStop() string {
	prefix := "The " + strings.Repeat("x ", 13)
	return prefix + strings.Repeat(" ", 101-len(prefix)-len("goals:")) + "goals:"
}
func cobitFixture() ([]string, []pdftext.Page) {
	l := make([]string, 296)
	w := make([]pdftext.Page, 296)
	s := "Governance Objective: EDM01 Invented title"
	l[28] = strings.Repeat("\n", 8) + s + strings.Repeat(" ", 115-len(s)) + "Focus Area: Example\nDescription\nInvented description\nPurpose\nInvented purpose\n" + cobitStop()
	w[28].Words = []pdftext.Word{cobitWord("40", "10", "A."), cobitWord("50", "10", "Invented"), cobitWord("80", "10", "Process"), cobitWord("40", "20", "Governance"), cobitWord("90", "20", "Practice"), cobitWord("305", "20", "Example"), cobitWord("345", "20", "Metrics"), cobitWord("40", "30", "EDM01.01"), cobitWord("110", "30", "Invented"), cobitWord("40", "32", "Invented"), cobitWord("90", "32", "description"), cobitWord("40", "36", "Activities"), cobitWord("500", "36", "Capability"), cobitWord("540", "36", "Level"), cobitWord("40", "40", "B."), cobitWord("50", "40", "Invented"), cobitWord("80", "40", "Example"), cobitWord("150", "40", "Structures"), cobitWord("40", "50", "EDM01.02")}
	return l, w
}
func TestCOBIT_Hierarchy(t *testing.T) {
	l, w := cobitFixture()
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	want := []Node{{Citation: "EDM", CitationNorm: "EDM", Kind: "domain", Parent: -1, Ordinal: 0, Status: "active"}, {Citation: "EDM01", CitationNorm: "EDM01", Kind: "objective", Parent: 0, Ordinal: 1, PublisherTitle: "Invented title", Body: "Description\nInvented description\n\nPurpose\nInvented purpose", Status: "active"}, {Citation: "EDM01.01", CitationNorm: "EDM01.01", Kind: "practice", Parent: 1, Ordinal: 2, PublisherTitle: "Invented", Body: "Invented description", Status: "active"}}
	if err != nil || got == nil || !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
}
func TestCOBIT_DomainParents(t *testing.T) {
	l, w := cobitFixture()
	baseL, baseW := l[28], w[28]
	var want []Node
	for i, domain := range []string{"EDM", "APO", "BAI", "DSS", "MEA"} {
		l[28+i] = strings.ReplaceAll(baseL, "EDM01", domain+"01")
		w[28+i].Words = append([]pdftext.Word(nil), baseW.Words...)
		for j := range w[28+i].Words {
			w[28+i].Words[j].Text = strings.ReplaceAll(w[28+i].Words[j].Text, "EDM01", domain+"01")
		}
		root := len(want)
		want = append(want, Node{Citation: domain, CitationNorm: domain, Kind: "domain", Parent: -1, Ordinal: root, Status: "active"}, Node{Citation: domain + "01", CitationNorm: domain + "01", Kind: "objective", Parent: root, Ordinal: root + 1, PublisherTitle: "Invented title", Body: "Description\nInvented description\n\nPurpose\nInvented purpose", Status: "active"}, Node{Citation: domain + "01.01", CitationNorm: domain + "01.01", Kind: "practice", Parent: root + 1, Ordinal: root + 2, PublisherTitle: "Invented", Body: "Invented description", Status: "active"})
	}
	expected := len(want)
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || got == nil || !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestCOBIT_DescriptionLabel(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "Description", "Description:", 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted malformed description label")
	}
}

func TestCOBIT_DuplicatePractice(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = w[28].Words[:14]
	w[28].Words = append(w[28].Words, cobitWord("40", "42", "Governance"), cobitWord("90", "42", "Practice"), cobitWord("305", "42", "Example"), cobitWord("345", "42", "Metrics"), cobitWord("40", "45", "EDM01.01"))
	expected := 3
	_, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err == nil || !strings.Contains(err.Error(), "duplicate practice") {
		t.Fatalf("wrong duplicate error: %v", err)
	}
}
func TestCOBIT_DeterminismAndErrorPrivacy(t *testing.T) {
	l, w := cobitFixture()
	expected := 3
	a, e := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if e != nil {
		t.Fatal(e)
	}
	b, e := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("non-deterministic results")
	}
	const sentinel = "invented-private-sentinel"
	w[28].Words[7].Text = sentinel
	_, e = parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if e == nil || strings.Contains(e.Error(), sentinel) {
		t.Fatal("error exposed input")
	}
}

func TestCOBIT_RejectsInvalid(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]string, []pdftext.Page)
		sha    string
		count  int
	}{
		{name: "wrong fingerprint", sha: "wrong", count: 3},
		{name: "wrong count", sha: cobitSHA, count: 4},
		{name: "invalid coordinate", sha: cobitSHA, count: 3, change: func(_ []string, w []pdftext.Page) { w[28].Words[0].YMin = "NaN" }},
		{name: "duplicate objective", sha: cobitSHA, count: 3, change: func(l []string, _ []pdftext.Page) { l[29] = l[28] }},
		{name: "oversized input", sha: cobitSHA, count: 3, change: func(l []string, _ []pdftext.Page) { l[0] = strings.Repeat("x", 50_000_001) }},

		{name: "wrong parent", sha: cobitSHA, count: 3, change: func(_ []string, w []pdftext.Page) { w[28].Words[7].Text = "APO01.01" }},
		{name: "missing metrics", sha: cobitSHA, count: 3, change: func(_ []string, w []pdftext.Page) { w[28].Words[5].Text = "Other" }},
		{name: "intervening left cell", sha: cobitSHA, count: 3, change: func(_ []string, w []pdftext.Page) {
			w[28].Words = append(w[28].Words, cobitWord("40", "25", "Invented"))
		}},
		{name: "displaced practice identifier", sha: cobitSHA, count: 3, change: func(_ []string, w []pdftext.Page) { w[28].Words[7].XMin = "45"; w[28].Words[7].XMax = "45" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			if tc.change != nil {
				tc.change(l, w)
			}
			if _, err := parseCOBIT(l, w, 1994, tc.sha, &tc.count); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}
