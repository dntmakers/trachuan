package parse

import (
	"strings"
	"testing"
)

const inventedTable = "  Control type  Alpha  Beta  Gamma  Delta"

func TestISO27002_LayoutGuards(t *testing.T) {
	page := "5 Domain\n5.1   First\n" + inventedTable + "\nControl\nStatement.\nGuidance\nAnnex A\nControl types vary\n5.2 Mention\n  Control types vary here\n42\nText mentioning © ISO/IEC 2022 – All rights reserved within a sentence.\nLeft  Right\nOne  Two\n                 Annex A\n"
	count := 2
	got, err := parseSyntheticPages("iso27002-2022-pdf", []string{page}, "", "", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := "Control\nStatement.\n\nGuidance\nAnnex A Control types vary 5.2 Mention Control types vary here 42 Text mentioning © ISO/IEC 2022 – All rights reserved within a sentence.\nLeft Right One Two"
	if got.Nodes[1].Body != want {
		t.Fatalf("body %q", got.Nodes[1].Body)
	}
}

func TestISO27002_SectionGuards(t *testing.T) {
	for _, tc := range []struct{ name, sections, want string }{
		{"empty control", "Control\nPurpose\nText.\n", "missing required section"},
		{"empty optional", "Control\nText.\nGuidance\n", "empty section"},
		{"repeated", "Control\nText.\nGuidance\nText.\nGuidance\nText.\n", "invalid section order"},
		{"out of order", "Control\nText.\nGuidance\nText.\nPurpose\nText.\n", "invalid section order"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 2
			_, err := parseSyntheticPages("iso27002-2022-pdf", []string{"5 Domain\n5.1 Title\n" + inventedTable + "\n" + tc.sections + "                 Annex A\n"}, "", "", &count)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}

func TestISO27002_NumericPageEdge(t *testing.T) {
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nStatement.\nGuidance\n                               42", "Continuation.\n                 Annex A"}
	count := 2
	got, err := parseSyntheticPages("iso27002-2022-pdf", pages, "", "", &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Control\nStatement.\n\nGuidance\n42 Continuation." {
		t.Fatalf("body %q", got.Nodes[1].Body)
	}
}

func TestISO27002_DomainRequiresFirstControl(t *testing.T) {
	count := 2
	_, err := parseSyntheticPages("iso27002-2022-pdf", []string{"5 Domain\n5.2 Title\n" + inventedTable + "\nControl\nText.\n                 Annex A\n"}, "", "", &count)
	if err == nil || err.Error() != "missing required region" {
		t.Fatalf("error: %v", err)
	}
}
