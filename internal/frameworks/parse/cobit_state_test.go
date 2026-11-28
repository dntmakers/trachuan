package parse

import (
	"danny.vn/trachuan/internal/pdftext"
	"strings"
	"testing"
)

func TestCOBIT_HeaderCannotCrossPage(t *testing.T) {
	l, w := cobitFixture()
	all := w[28].Words
	w[28].Words = append([]pdftext.Word(nil), all[:7]...)
	w[29].Words = append([]pdftext.Word(nil), all[7:]...)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted practice ID on next page")
	}
}
func TestCOBIT_EmptyObjectiveTitle(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "EDM01 Invented title", "EDM01               ", 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted empty objective title")
	}
}

func TestCOBIT_PageRequiresComponentA(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = w[28].Words[:14]
	w[29].Words = []pdftext.Word{cobitWord("40", "20", "Governance"), cobitWord("90", "20", "Practice"), cobitWord("305", "20", "Example"), cobitWord("345", "20", "Metrics"), cobitWord("40", "30", "EDM01.02"), cobitWord("100", "30", "Title"), cobitWord("40", "32", "Body"), cobitWord("40", "36", "Activities"), cobitWord("500", "36", "Capability"), cobitWord("540", "36", "Level"), cobitWord("40", "40", "B."), cobitWord("50", "40", "Component:"), cobitWord("80", "40", "Organizational"), cobitWord("150", "40", "Structures")}
	expected := 4
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted unanchored practice")
	}
}
func TestCOBIT_ClosedComponentCannotReopen(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "60", "A."), cobitWord("50", "60", "Component:"), cobitWord("80", "60", "Process"), cobitWord("40", "70", "B."), cobitWord("50", "70", "Component:"), cobitWord("80", "70", "Organizational"), cobitWord("150", "70", "Structures"))
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("reopened closed process")
	}
}
func TestCOBIT_MetricsActivityDoesNotClose(t *testing.T) {
	l, w := cobitFixture()
	for i := 11; i < 14; i++ {
		w[28].Words[i].YMin = "40"
		w[28].Words[i].YMax = "40"
	}
	for i := 14; i < 18; i++ {
		w[28].Words[i].YMin = "50"
		w[28].Words[i].YMax = "50"
	}
	w[28].Words[18].YMin = "60"
	w[28].Words[18].YMax = "60"
	w[28].Words = append(w[28].Words, cobitWord("305", "34", "Activities"), cobitWord("500", "34", "Capability"), cobitWord("540", "34", "Level"), cobitWord("40", "37", "Still"))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || !strings.Contains(got.Nodes[2].Body, "Still") {
		t.Fatal("metrics closed practice")
	}
}
func TestCOBIT_FalsePurposeBoundary(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], cobitStop(), strings.Replace(cobitStop(), "goals:", "nope!:", 1), 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted false purpose boundary")
	}
}

func TestCOBIT_MultipleOpeners(t *testing.T) {
	l, w := cobitFixture()
	lines := strings.Split(l[28], "\n")
	lines[5] = strings.Replace(lines[8], "EDM01", "EDM02", 1)
	lines[6] = "Description"
	lines[7] = "Invented"
	l[28] = strings.Join(lines, "\n")
	expected := 4
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted two objective openers")
	}
}
func TestCOBIT_DomainTextNotMargin(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "Invented description\nPurpose", "Invented description\nEDM\nPurpose", 1)
	expected := 3
	_, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err == nil {
		t.Fatal("accepted ambiguous domain-only field line")
	}
}
