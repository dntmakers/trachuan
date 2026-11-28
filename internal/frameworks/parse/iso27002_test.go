package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestISO27002_Pages(t *testing.T) {
	pages := []string{
		"ISO/IEC 27002:2022(E)\n5 Contents ................ 1\n",
		"ISO/IEC 27002:2022(E)\n5 Domain\n5.1 First\n",
		"   ISO/IEC 27002:2022(E)\nwrapped title\n" + inventedTable + "\n#invented\nControl\nA wrapped\n",
		"ISO/IEC 27002:2022(E)\nstatement.\na) First item\n continued;\nb) Second item\nPurpose\nReason.\nGuidance\nDetails.\nOther information\nReference.\n                                 1\n© ISO/IEC 2022 – All rights reserved 1\n",
		"6 Next domain\n6.1 Second\n" + inventedTable + "\n#invented\nControl\nSecond statement.\nGuidance\nAnother detail.\n                 Annex A\n5.3 Ignored\n",
	}
	count := 4
	got, err := parseSyntheticPages("iso27002-2022-pdf", pages, "iso27002", "2022", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{
		{Citation: "5", CitationNorm: "5", Kind: "domain", Parent: -1, Ordinal: 0, PublisherTitle: "Domain", Status: "active"},
		{Citation: "5.1", CitationNorm: "5.1", Kind: "control", Parent: 0, Ordinal: 1, PublisherTitle: "First wrapped title", Body: "Control\nA wrapped statement.\na) First item continued;\nb) Second item\n\nPurpose\nReason.\n\nGuidance\nDetails.\n\nOther information\nReference.", Status: "active"},
		{Citation: "6", CitationNorm: "6", Kind: "domain", Parent: -1, Ordinal: 2, PublisherTitle: "Next domain", Status: "active"},
		{Citation: "6.1", CitationNorm: "6.1", Kind: "control", Parent: 2, Ordinal: 3, PublisherTitle: "Second", Body: "Control\nSecond statement.\n\nGuidance\nAnother detail.", Status: "active"},
	}
	if !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("nodes: %#v", got.Nodes)
	}
	again, err := parseSyntheticPages("iso27002-2022-pdf", pages, "iso27002", "2022", &count)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("nondeterministic")
	}
}

func TestISO27002_Errors(t *testing.T) {
	const sentinel = "PRIVATE_SENTINEL"
	table := inventedTable + "\nControl\n" + sentinel + "\n"
	cases := []struct {
		name, page, want string
		count            int
	}{
		{"missing annex", "5 Domain\n5.1 Title\n" + table, "missing required region", 2},
		{"duplicate", "5 Domain\n5.1 Title\n" + table + "5.1 Title\n" + table + "                 Annex A\n", "invalid or duplicate citations", 3},
		{"missing parent", "5 Domain\n5.1 Title\n" + table + "6.1 Title\n" + table + "                 Annex A\n", "missing control parent", 3},
		{"count mismatch", "5 Domain\n5.1 Title\n" + table + "                 Annex A\n", "control count mismatch", 99},
		{"missing start", "6 Domain\n6.1 Title\n" + table + "                 Annex A\n", "missing required region", 2},
		{"missing statement", "5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nPurpose\n" + sentinel + "\n                 Annex A\n", "missing required section", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSyntheticPages("iso27002-2022-pdf", []string{tc.page}, "iso27002", "2022", &tc.count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), sentinel) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}
