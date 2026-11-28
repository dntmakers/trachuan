package parse

import (
	"reflect"
	"strings"
	"testing"
)

const iso27018TestSHA = "83e63bb508e90238d101d964d6b0d00eabe1e564e32a697181a7dc230ccf7cdf"

func TestISO27018_Headings(t *testing.T) {
	pages := []string{"5 Contents ........ 1\n\n5 Domain\n\n5.1 First\n\n5.1.1 Child\nWords.\n\n6 Root\nA reference.\n\n                                               Annex A\n\nA.1 Domain\nA.1.1 Child\nControl\nWords.\n\n                                           Bibliography\n"}
	count := 6
	got, err := ParsePages("iso27018-2019-pdf", pages, "", "", iso27018TestSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	citations := []string{"5", "5.1", "5.1.1", "6", "A.1", "A.1.1"}
	kinds := []string{"control", "control", "control", "control", "domain", "annex-control"}
	parents := []int{-1, 0, 1, -1, -1, 4}
	for i, n := range got.Nodes {
		if n.Citation != citations[i] || n.Kind != kinds[i] || n.Parent != parents[i] {
			t.Fatalf("node %d has wrong identity or parent", i)
		}
	}
	again, err := ParsePages("iso27018-2019-pdf", pages, "", "", iso27018TestSHA, &count)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("nondeterministic result")
	}
	count++
	_, err = ParsePages("iso27018-2019-pdf", pages, "", "", iso27018TestSHA, &count)
	if err == nil || err.Error() != "control count mismatch" {
		t.Fatalf("unexpected count error: %v", err)
	}
}

func TestISO27018_Errors(t *testing.T) {
	for _, tc := range []struct{ name, page, sha, want string }{
		{"wrong hash", "", "wrong", "source fingerprint mismatch"},
		{"missing region", "SECRET", iso27018TestSHA, "missing required region"},
		{"missing annex", "5 Root\n\n5.1 One\n                                           Bibliography\n", iso27018TestSHA, "missing required region"},
		{"missing parent", "5 Root\n\n5.1 One\n\n5.2.1 Orphan\n                                               Annex A\n                                           Bibliography\n", iso27018TestSHA, "missing control parent"},
		{"duplicate", "5 Root\n\n5.1 One\n\n5.1 Two\n                                               Annex A\n                                           Bibliography\n", iso27018TestSHA, "invalid or duplicate citations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 3
			_, err := ParsePages("iso27018-2019-pdf", []string{tc.page}, "", "", tc.sha, &count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27018_HeadingBoundaries(t *testing.T) {
	page := "5 Root\n\n5.1 First\n5.2 Inline reference\n\n5.3 Sentence.\n\n5.4 Contents .... 2\n                                               Annex A\n\nA.1 Domain\n\nA.1.1 Reference\nNot a control label.\n                                           Bibliography\n"
	count := 3
	got, err := ParsePages("iso27018-2019-pdf", []string{page}, "", "", iso27018TestSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	for i, citation := range []string{"5", "5.1", "A.1"} {
		if got.Nodes[i].Citation != citation {
			t.Fatalf("unexpected citation at %d", i)
		}
	}
}

func TestISO27018_Bodies(t *testing.T) {
	pages := []string{
		"introductory illustration\n5 Contents .... 1\n\n5 Root\n\n5.1 Reference\nSee another invented edition.\n\nA new paragraph\ncontinues here.\n",
		"Continued on another page.\na) First item\nwrapped item\n— Second item\nNOTE An invented note\ncontinues.\nEXAMPLE An example.\n                                               Annex A\nannex preamble\n\nA.1 Domain\nDomain introduction.\n\nA.1.1 Child\nControl\nA statement\ncontinues.\n\nOther information for cloud PII\nExtra information.\n                                           Bibliography\nbibliography entry\n",
	}
	count := 4
	got, err := ParsePages("iso27018-2019-pdf", pages, "", "", iso27018TestSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{
		{Citation: "5", CitationNorm: "5", Kind: "control", Parent: -1, Ordinal: 0, PublisherTitle: "Root", Status: "active"},
		{Citation: "5.1", CitationNorm: "5.1", Kind: "control", Parent: 0, Ordinal: 1, PublisherTitle: "Reference", Body: "See another invented edition.\n\nA new paragraph continues here.\n\nContinued on another page.\na) First item wrapped item\n— Second item\n\nNOTE An invented note continues.\n\nEXAMPLE An example.", Status: "active"},
		{Citation: "A.1", CitationNorm: "A.1", Kind: "domain", Parent: -1, Ordinal: 2, PublisherTitle: "Domain", Body: "Domain introduction.", Status: "active"},
		{Citation: "A.1.1", CitationNorm: "A.1.1", Kind: "annex-control", Parent: 2, Ordinal: 3, PublisherTitle: "Child", Body: "Control\nA statement continues.\n\nOther information for cloud PII\nExtra information.", Status: "active"},
	}
	if !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("unexpected nodes: %#v", got.Nodes)
	}
}

func TestISO27018_EmptyRequiredBody(t *testing.T) {
	for _, tc := range []struct{ name, main, annex string }{
		{"main leaf", "5 Root\n\n5.1 Empty\n", "A.1 Domain\nA.1.1 Child\nControl\nStatement.\n"},
		{"annex control", "5 Root\n\n5.1 Reference\nA reference.\n", "A.1 Domain\nA.1.1 Child\nControl\n"},
		{"missing statement before information", "5 Root\n\n5.1 Reference\nA reference.\n", "A.1 Domain\nA.1.1 Child\nControl\n\nOther information for cloud PII\nDetails.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 4
			page := tc.main + "                                               Annex A\n\n" + tc.annex + "                                           Bibliography\n"
			_, err := ParsePages("iso27018-2019-pdf", []string{page}, "", "", iso27018TestSHA, &count)
			if err == nil || err.Error() != "missing required body" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27018_ListMarkers(t *testing.T) {
	page := "5 Root\n\n5.1 First\nAlpha\n1) Numeric\n• Bullet\nb) Letter\n— Dash\n                                               Annex A\n\nA.1 Domain\nA.1.1 Child\nControl\nStatement.\n                                           Bibliography\n"
	count := 4
	got, err := ParsePages("iso27018-2019-pdf", []string{page}, "", "", iso27018TestSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Alpha 1) Numeric • Bullet\nb) Letter\n— Dash" {
		t.Fatalf("unexpected body: %q", got.Nodes[1].Body)
	}
}

func TestISO27018_ZeroWidthFurniture(t *testing.T) {
	bom := string(rune(0xfeff))
	page := "5 Root\n\n5.1 First\nAlpha" + bom + " beta.\n" + bom + "\n                                               Annex A\n\nA.1 Domain\nA.1.1 Child\nControl\nStatement.\n                                           Bibliography\n"
	count := 4
	got, err := ParsePages("iso27018-2019-pdf", []string{page}, "", "", iso27018TestSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Alpha beta." {
		t.Fatalf("unexpected body: %q", got.Nodes[1].Body)
	}
}
