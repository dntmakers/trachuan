package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestISO27001_RejectsWrongFingerprint(t *testing.T) {
	count := 1
	_, err := ParsePages("iso27001-2022-pdf", nil, "iso27001", "2022", "wrong", &count)
	if err == nil || err.Error() != "source fingerprint mismatch" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestISO27001_HeadingsAndParents(t *testing.T) {
	pages := []string{
		"4 Contents .... 1\n\n4   Alpha\n\n4.1   Beta\nBody text.\n\n4.1.1   Gamma\nMore text.\n\n5   Delta\nRoot text.\n       Annex A\n       Table A.1 - Fictional\n5      Domain\n5.1    Child                       Control\n                                   Text.\n       Bibliography\n",
	}
	count := 6
	got, err := ParsePages("iso27001-2022-pdf", pages, "", "", "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		citation, kind string
		parent         int
	}{{"4", "clause", -1}, {"4.1", "clause", 0}, {"4.1.1", "clause", 1}, {"5", "clause", -1}, {"A.5", "domain", -1}, {"A.5.1", "annex-control", 4}}
	if len(got.Nodes) != len(want) {
		t.Fatalf("got %d nodes", len(got.Nodes))
	}
	for i, node := range got.Nodes {
		if node.Citation != want[i].citation || node.Kind != want[i].kind || node.Parent != want[i].parent {
			t.Fatalf("node %d: %#v", i, node)
		}
	}
}

func TestISO27001_Errors(t *testing.T) {
	const fingerprint = "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54"
	const sentinel = "PRIVATE_SENTINEL"
	prefix := "4   Alpha\n\n4.1   Beta\n" + sentinel + "\n"
	end := "       Annex A\n       Table A.1 - Fictional\n5      Domain\n5.1    Child                       Control\n                                   Text.\n       Bibliography\n"
	cases := []struct {
		name, text, want string
		count            int
	}{
		{"missing region", prefix, "missing required region", 2},
		{"duplicate clause", prefix + "\n4.1   Repeated\n" + end, "invalid or duplicate citations", 5},
		{"missing clause parent", prefix + "\n4.2.1   Orphan\n" + end, "missing parent", 5},
		{"wrong active clause root", prefix + "\n5   Other\n\n4.1.1   Orphan\n" + end, "missing parent", 6},
		{"wrong active annex root", prefix + "       Annex A\n       Table A.1 - Fictional\n5      Domain\n6      Other\n5.1    Orphan                      Control\n       Bibliography\n", "missing parent", 5},
		{"missing annex parent", prefix + "       Annex A\n       Table A.1 - Fictional\n6.1    Orphan                      Control\n       Bibliography\n", "missing parent", 3},
		{"count mismatch", prefix + end, "control count mismatch", 99},
		{"empty leaf clause", "4   Alpha\n\n4.1   Beta\n" + end, "missing clause body", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePages("iso27001-2022-pdf", []string{tc.text}, "", "", fingerprint, &tc.count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), sentinel) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27001_BodiesAcrossPages(t *testing.T) {
	pages := []string{
		"4 Contents .... 1\n\n4   Alpha\n\n4.1   Beta\nFirst sentence.\nWrapped sentence.\n\nNOTE Invented note.\n\n       Annex A\nIntroductory material.\n       Table A.1 - Fictional\n5      Domain\n5.1    Long child                  Control\n       continued title\n                                   First portion\n© ISO/IEC 2022 – All rights reserved 1\n",
		"ISO/IEC 27001:2022(E)\n       Table A.1 (continued)\n                                   second portion.\n       Bibliography\nExcluded.\n",
	}
	count := 4
	got, err := ParsePages("iso27001-2022-pdf", pages, "", "", "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{
		{Citation: "4", CitationNorm: "4", Kind: "clause", Parent: -1, Ordinal: 0, PublisherTitle: "Alpha", Status: "active"},
		{Citation: "4.1", CitationNorm: "4.1", Kind: "clause", Parent: 0, Ordinal: 1, PublisherTitle: "Beta", Body: "First sentence. Wrapped sentence.\n\nNOTE Invented note.", Status: "active"},
		{Citation: "A.5", CitationNorm: "A.5", Kind: "domain", Parent: -1, Ordinal: 2, PublisherTitle: "Domain", Status: "active"},
		{Citation: "A.5.1", CitationNorm: "A.5.1", Kind: "annex-control", Parent: 2, Ordinal: 3, PublisherTitle: "Long child continued title", Body: "First portion second portion.", Status: "active"},
	}
	if !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("unexpected nodes: %#v", got.Nodes)
	}
	again, err := ParsePages("iso27001-2022-pdf", pages, "", "", "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54", &count)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("nondeterministic output")
	}
}

func TestISO27001_RejectsMalformedCells(t *testing.T) {
	prefix := "4   Alpha\n\n4.1   Beta\nBody.\n       Annex A\n       Table A.1 - Fictional\n5      Domain\n"
	cases := []struct{ name, row, want string }{
		{"empty statement", "5.1    Child                       Control\n", "missing annex statement"},
		{"outside columns", "5.1    Child                       Control\n  PRIVATE_SENTINEL\n", "invalid annex continuation"},
		{"continuation without child", "                                   PRIVATE_SENTINEL\n", "invalid annex continuation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := 4
			_, err := ParsePages("iso27001-2022-pdf", []string{prefix + tc.row + "       Bibliography\n"}, "", "", "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54", &count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27001_RejectsGluedControlLabel(t *testing.T) {
	page := "4   Alpha\n\n4.1   Beta\nBody.\n       Annex A\n       Table A.1 - Fictional\n5      Domain\n5.1    ABCDEFGHIJKLMNOPQRSTUVWXYZABControl\n                                   Statement.\n       Bibliography\n"
	count := 4
	_, err := ParsePages("iso27001-2022-pdf", []string{page}, "", "", "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54", &count)
	if err == nil || err.Error() != "invalid annex continuation" {
		t.Fatalf("unexpected error: %v", err)
	}
}
