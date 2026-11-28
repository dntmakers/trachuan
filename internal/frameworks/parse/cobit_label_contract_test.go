package parse

import (
	"strings"
	"testing"
)

func TestCOBIT_ComponentLabelShapes(t *testing.T) {
	cases := []struct {
		name  string
		words []string
		want  int
	}{
		{"component A", []string{"A.", "middle", "Process"}, 1},
		{"component A continuation", []string{"A.", "middle", "other", "(cont.)"}, 2},
		{"component B", []string{"B.", "middle", "other", "Structures"}, 3},
		{"component B continuation", []string{"B.", "middle", "other", "more", "(cont.)"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := cobitRow{}
			for i, s := range tc.words {
				row.words = append(row.words, cobitBox{x: 40 + float64(i)*10, text: s})
			}
			if row.component() != tc.want {
				t.Fatal("valid shape rejected")
			}
			for _, i := range []int{0, len(row.words) - 1} {
				bad := cobitRow{words: append([]cobitBox(nil), row.words...)}
				bad.words[i].text = strings.ToLower(bad.words[i].text)
				if bad.words[i].text == row.words[i].text {
					bad.words[i].text += "!"
				}
				if bad.component() == tc.want {
					t.Fatal("accepted altered edge")
				}
			}
			row.words = append(row.words, cobitBox{x: 100, text: "extra"})
			if row.component() == tc.want {
				t.Fatal("accepted extra box")
			}
		})
	}
}
func TestCOBIT_PracticeAndMetricsLabelShapes(t *testing.T) {
	base := cobitRow{words: []cobitBox{{x: 40, text: "Governance"}, {x: 80, text: "Practice"}, {x: 305, text: "Example"}, {x: 350, text: "Metrics"}}}
	for _, index := range []int{0, 1, 2, 3} {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			r := cobitRow{words: append([]cobitBox(nil), base.words...)}
			r.words[index].text = strings.ToLower(r.words[index].text)
			_, _, ok, _ := r.practiceHeader()
			if ok {
				t.Fatal("accepted changed edge")
			}
		})
	}
	r := cobitRow{words: []cobitBox{{x: 40, text: "Governance Practice"}, {x: 305, text: "Example"}, {x: 350, text: "Metrics"}}}
	if _, _, ok, _ := r.practiceHeader(); ok {
		t.Fatal("accepted combined box")
	}
}
func TestCOBIT_ActivityAndCapabilityShapes(t *testing.T) {
	base := cobitRow{words: []cobitBox{{x: 40, text: "Activities"}, {x: 500, text: "Capability"}, {x: 540, text: "Level"}}}
	for _, index := range []int{0, 1, 2} {
		r := cobitRow{words: append([]cobitBox(nil), base.words...)}
		r.words[index].text = strings.ToLower(r.words[index].text)
		if r.activity() {
			t.Fatal("accepted altered edge")
		}
	}
	for _, extra := range []cobitBox{{x: 45, text: "extra"}, {x: 520, text: "extra"}} {
		row := cobitRow{words: append(append([]cobitBox(nil), base.words...), extra)}
		if row.activity() {
			t.Fatal("accepted extra label box")
		}
	}
	r := cobitRow{words: []cobitBox{{x: 40, text: "Activities"}, {x: 500, text: "Capability Level"}}}
	if r.activity() {
		t.Fatal("accepted combined capability box")
	}
}
func TestCOBIT_FieldLabelsTrailingSpaces(t *testing.T) {
	for _, label := range []string{"Description", "Purpose"} {
		t.Run(label, func(t *testing.T) {
			l, w := cobitFixture()
			l[28] = strings.Replace(l[28], "\n"+label+"\n", "\n"+label+"   \n", 1)
			expected := 3
			if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCOBIT_ObjectiveAndFieldLabels(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		valid          bool
	}{
		{"objective case", "Governance Objective:", "governance objective:", true},
		{"objective spacing", "Governance Objective:", "Governance  Objective:", true},
		{"objective first word", "Governance Objective:", "Other Objective:", false},
		{"objective last word", "Governance Objective:", "Governance Other:", false},
		{"objective extra word", "Governance Objective:", "Governance Extra Objective:", false},
		{"description label", "\nDescription\n", "\nDescription Extra\n", false},
		{"purpose label", "\nPurpose\n", "\nPurpose Extra\n", false},
		{"focus extra word", "Focus Area:", "Focus Other Area:", false},
		{"focus last word", "Focus Area:", "Focus Area;", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			l[28] = strings.Replace(l[28], tc.old, tc.new, 1)
			expected := 3
			_, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected acceptance: %v", err)
			}
		})
	}
}
func TestCOBIT_PurposeBoundaryShape(t *testing.T) {
	for _, bad := range []string{strings.Replace(cobitStop(), "The", "the", 1), strings.Replace(cobitStop(), "goals:", "goals;", 1), strings.Replace(cobitStop(), "x x", "xx ", 1)} {
		if cobitPurposeBoundary(bad) {
			t.Fatal("accepted invalid purpose boundary")
		}
	}
}

func TestCOBIT_TwoFooterRows(t *testing.T) {
	l, w := cobitFixture()
	w[28].Words = append(w[28].Words, cobitWord("40", "742", "12"), cobitWord("40", "744", "13"))
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted multiple footer rows")
	}
}

func TestCOBIT_LayoutAndFooterShapes(t *testing.T) {
	if cobitFocusColumn(strings.Repeat(" ", 113)+"focus area: value") >= 0 {
		t.Fatal("accepted focus case change")
	}
	if !cobitPurposeBoundary(cobitStop() + "   ") {
		t.Fatal("rejected trailing spaces")
	}
	for _, s := range []string{"+12", "-1"} {
		if (cobitRow{y: 743, words: []cobitBox{{x: 40, text: s}}}).footer() {
			t.Fatal("accepted signed footer")
		}
	}
}
