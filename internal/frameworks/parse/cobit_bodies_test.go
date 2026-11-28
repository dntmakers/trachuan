package parse

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestCOBIT_Bodies(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "38", "excluded"))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Description\nInvented description\n\nPurpose\nInvented purpose" {
		t.Fatalf("objective: %q", got.Nodes[1].Body)
	}
	if got.Nodes[2].PublisherTitle != "Invented" || got.Nodes[2].Body != "Invented description" {
		t.Fatalf("practice: %#v", got.Nodes[2])
	}
}
func TestCOBIT_ActivityWordInDescription(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "34", "Activities"), cobitWord("95", "34", "continue"))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || got.Nodes[2].Body != "Invented description Activities continue" {
		t.Fatalf("unexpected result: %#v %v", got, err)
	}
}

func TestCOBIT_Continuation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		header, extra bool
		wantError     bool
	}{
		{name: "repeated component and activity", header: true},
		{name: "missing component", wantError: true},
		{name: "extra line", header: true, extra: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			w[28].Words = w[28].Words[:11]
			if tc.header {
				w[29].Words = append(w[29].Words, cobitWord("40", "80", "A."), cobitWord("50", "80", "Invented"), cobitWord("80", "80", "Process"), cobitWord("120", "80", "(cont.)"))
			}
			if tc.extra {
				w[29].Words = append(w[29].Words, cobitWord("40", "90", "unexpected"))
			}
			w[29].Words = append(w[29].Words, cobitWord("40", "100", "Activities"), cobitWord("500", "100", "Capability"), cobitWord("540", "100", "Level"), cobitWord("40", "120", "B."), cobitWord("50", "120", "Invented"), cobitWord("80", "120", "Example"), cobitWord("150", "120", "Structures"))
			expected := 3
			got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
			if tc.wantError {
				if err == nil {
					t.Fatal("accepted invalid continuation")
				}
				return
			}
			if err != nil || got.Nodes[2].Body != "Invented description" {
				t.Fatalf("unexpected result: %#v %v", got, err)
			}
		})
	}
}

func TestCOBIT_RejectsInvalidBodies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]string, []pdftext.Page)
	}{
		{"empty objective", func(l []string, _ []pdftext.Page) {
			l[28] = strings.Replace(l[28], "Invented description\nPurpose", "Purpose", 1)
		}},
		{"empty practice", func(_ []string, w []pdftext.Page) { w[28].Words[9].Text = ""; w[28].Words[10].Text = "" }},
		{"crossing word", func(_ []string, w []pdftext.Page) { w[28].Words[9].XMax = "303" }},
		{"indented description", func(l []string, _ []pdftext.Page) {
			l[28] = strings.Replace(l[28], "\nDescription\n", "\n Description\n", 1)
		}},
		{"repeated component before activity", func(_ []string, w []pdftext.Page) {
			w[28].Words = append(w[28].Words, cobitWord("40", "34", "A."), cobitWord("50", "34", "Invented"), cobitWord("80", "34", "Process"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			tc.change(l, w)
			expected := 3
			if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
				t.Fatal("accepted invalid body")
			}
		})
	}
}

func TestCOBIT_ExplicitLists(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "34", "•"), cobitWord("50", "34", "Example"))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || got.Nodes[2].Body != "Invented description\n• Example" {
		t.Fatalf("unexpected result: %#v %v", got, err)
	}
}

func TestCOBIT_DropsMarginAndFooter(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "Invented description\nPurpose", "Invented description\nEDM\nPurpose", 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted ambiguous domain-only field line")
	}
}

func TestCOBIT_OrphanContinuation(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("120", "10", "(cont.)"))
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted orphan continuation")
	}
}

func TestCOBIT_AmbiguousFocusField(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "Focus Area: Example", "Focus Area: Example Focus Area: Other", 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted duplicate focus fields")
	}
}

func TestCOBIT_UnknownFooter(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "740", "invented"))
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted unexpected footer")
	}
}

func TestCOBIT_RequiresComponentEnd(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = w[28].Words[:14]
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted missing component B")
	}
}

func TestCOBIT_InvalidComponentLabel(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words[2].Text = "ProcessExtra"
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted malformed component label")
	}
}

func TestCOBIT_AmbiguousMetrics(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("315", "20", "Example"))
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted ambiguous metrics header")
	}
}
func TestCOBIT_ActivityWordsAreNotHeader(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "34", "Invented"), cobitWord("80", "34", "Activities"), cobitWord("500", "34", "Capability"), cobitWord("540", "34", "Level"))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || !strings.Contains(got.Nodes[2].Body, "Invented Activities") {
		t.Fatal("description was truncated")
	}
}
func TestCOBIT_UnicodeColumns(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.Replace(l[28], "Invented title", "Invénted title", 1)
	l[28] = strings.Replace(l[28], cobitStop(), strings.Replace(cobitStop(), "x", "é", 1), 1)
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err != nil {
		t.Fatal(err)
	}
}
func TestCOBIT_ContinuationParentAndPage(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		missingPage, otherObjective bool
	}{{"missing page", true, false}, {"other objective", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			w[28].Words = w[28].Words[:11]
			next := 29
			if tc.missingPage {
				next = 30
			}
			if tc.otherObjective {
				l[29] = strings.ReplaceAll(l[28], "EDM01", "EDM02")
			}
			w[next].Words = []pdftext.Word{cobitWord("40", "80", "A."), cobitWord("50", "80", "Invented"), cobitWord("80", "80", "Process"), cobitWord("40", "100", "Activities"), cobitWord("500", "100", "Capability"), cobitWord("540", "100", "Level"), cobitWord("40", "120", "B."), cobitWord("50", "120", "Invented"), cobitWord("80", "120", "Example"), cobitWord("150", "120", "Structures")}
			expected := 3
			if tc.otherObjective {
				expected = 4
			}
			if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
				t.Fatal("accepted invalid continuation")
			}
		})
	}
}

func TestCOBIT_EmptyBoxBeforeID(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("39.5", "30", ""))
	expected := 3
	got, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
	if err != nil || got.Nodes[2].PublisherTitle != "Invented" {
		t.Fatalf("unexpected title: %#v %v", got, err)
	}
}

func TestCOBIT_ReplacementCount(t *testing.T) {
	l, w := cobitFixture()
	expected := 3
	if _, err := parseCOBIT(l, w, 1993, cobitSHA, &expected); err == nil {
		t.Fatal("accepted replacement count")
	}
}
func TestCOBIT_RejectsReplacementInPractice(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, pdftext.Word{XMin: "40", YMin: "32", XMax: "41", YMax: "33", Text: "�"})
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted replacement character")
	}
}
