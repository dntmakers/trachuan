package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestISO27017_BodySectionsAndPages(t *testing.T) {
	pages := []string{
		"Comparison material\n    Column A   Column B\n    NOT RETAINED   NOT RETAINED\n",
		"5   Root\n\n5.1   Branch\nDirect sentence.\nSame paragraph\nwraps here.\n\nSecond paragraph;\n",
		"ISO/IEC 27017:2015 (E)\nContinuation after page.\n– List item\nNOTE A note\ncontinues.\nImplementation guidance for cloud services\n    cloud service customer   cloud service provider\n    EXCLUDED LEFT             EXCLUDED RIGHT\nOther information for cloud services\nDetails continue.\n\n" + strings.Repeat(" ", 60) + "Annex A\nPreface excluded.\n\nCLD.1.1 Root\nReference only.\n\nCLD.1.1.1 Child\nControl\nStatement.\n\n" + strings.Repeat(" ", 59) + "Annex B\n",
	}
	count := 4
	result, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{
		{Citation: "5", CitationNorm: "5", Kind: "control", Parent: -1, Ordinal: 0, PublisherTitle: "Root", Status: "active"},
		{Citation: "5.1", CitationNorm: "5.1", Kind: "control", Parent: 0, Ordinal: 1, PublisherTitle: "Branch", Body: "Direct sentence. Same paragraph wraps here.\n\nSecond paragraph;\n\nContinuation after page.\n– List item\n\nNOTE A note continues.\n\nOther information for cloud services\nDetails continue.", Status: "active"},
		{Citation: "CLD.1.1", CitationNorm: "CLD.1.1", Kind: "control", Parent: -1, Ordinal: 2, PublisherTitle: "Root", Body: "Reference only.", Status: "active"},
		{Citation: "CLD.1.1.1", CitationNorm: "CLD.1.1.1", Kind: "control", Parent: 2, Ordinal: 3, PublisherTitle: "Child", Body: "Control\nStatement.", Status: "active"},
	}
	if !reflect.DeepEqual(result.Nodes, want) {
		t.Fatalf("got %#v want %#v", result.Nodes, want)
	}
}

func TestISO27017_ConsecutiveLabelsAndPageList(t *testing.T) {
	pages := []string{
		"5   Root\n\n5.1   Branch\nControl\nOther information for cloud services\nText.\n",
		"– Item\n" + strings.Repeat(" ", 60) + "Annex A\n" + strings.Repeat(" ", 59) + "Annex B\n",
	}
	count := 2
	got, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Control\n\nOther information for cloud services\nText.\n– Item" {
		t.Fatalf("unexpected body: %q", got.Nodes[1].Body)
	}
}

func TestISO27017_TableContinuationAndHeading(t *testing.T) {
	pages := []string{
		"5   Root\n\n5.1   Branch\nReference text.\nImplementation guidance for cloud services\n    cloud service customer   cloud service provider\n    First column               Second column\n5 ITU-T X.1631 (07/2015)\n",
		"ISO/IEC 27017:2015 (E)\n    cloud service customer   cloud service provider\n    Continued cell             Another cell\n\n5.2   Next\nNext reference.\n" + strings.Repeat(" ", 60) + "Annex A\n" + strings.Repeat(" ", 59) + "Annex B\nITU-T X.1631 (07/2015) 6\n",
	}
	count := 3
	got, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Reference text." || got.Nodes[2].Body != "Next reference." {
		t.Fatal("table continuation or footer entered body")
	}
}

func TestISO27017_InvalidTable(t *testing.T) {
	cases := []struct{ name, table, want string }{
		{"missing role headers", "Implementation guidance for cloud services\nSENTINEL\n", "invalid guidance table header"},
		{"unexpected flush line", "Implementation guidance for cloud services\n    cloud service customer   cloud service provider\nSENTINEL\n", "invalid guidance table"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := 2
			_, err := ParsePages("iso27017-2015-pdf", []string{"5   Root\n\n5.1   Branch\n" + tc.table + strings.Repeat(" ", 60) + "Annex A\n" + strings.Repeat(" ", 59) + "Annex B\n"}, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "SENTINEL") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
