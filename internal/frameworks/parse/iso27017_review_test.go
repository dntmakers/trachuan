package parse

import (
	"runtime"
	"strings"
	"testing"
)

func TestISO27017_RejectEmptyLeaf(t *testing.T) {
	annexA := strings.Repeat(" ", 60) + "Annex A\n"
	annexB := strings.Repeat(" ", 59) + "Annex B\n"
	cases := []struct {
		name, text string
		count      int
	}{
		{"main control", "5   Root\n\n5.1   Empty\n\n5.2   Present\nReference.\n" + annexA + annexB, 3},
		{"annex root", "5   Root\n\n5.1   Present\nReference.\n" + annexA + "\nCLD.1.1 Empty\n" + annexB, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePages("iso27017-2015-pdf", []string{tc.text}, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &tc.count)
			if err == nil || err.Error() != "missing control body" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27017_EmptyChildControl(t *testing.T) {
	start := "5   Root\n\n5.1   Branch\n" + strings.Repeat(" ", 60) + "Annex A\n\nCLD.1.1 Root\n\nCLD.1.1.1 Child\nControl\n"
	end := strings.Repeat(" ", 59) + "Annex B\n"
	cases := []struct{ name, rest string }{
		{"before next heading", "\nCLD.1.1.2 Child\nControl\nText.\n" + end},
		{"before next section", "Other information for cloud services\nText.\n" + end},
		{"before guidance table", "Implementation guidance for cloud services\n    cloud service customer   cloud service provider\n    Cell   Cell\n" + end},
		{"before region end", end},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := 4
			_, err := ParsePages("iso27017-2015-pdf", []string{start + tc.rest}, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
			if err == nil || err.Error() != "missing control statement" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27017_NumericBodyBeforeFooter(t *testing.T) {
	pages := []string{
		"5   Root\n\n5.1   Branch\nReference\n42\n5 ITU-T X.1631 (07/2015)\n",
		"5.2   Next\nText.\n" + strings.Repeat(" ", 60) + "Annex A\n" + strings.Repeat(" ", 59) + "Annex B\n",
	}
	count := 3
	got, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Reference 42" {
		t.Fatalf("unexpected body: %q", got.Nodes[1].Body)
	}
}

func TestISO27017_FooterRequiresFinalLineAndPageNumber(t *testing.T) {
	cases := []struct{ name, tail, body string }{
		{"not final", "5 ITU-T X.1631 (07/2015)\nLast line.\n", "5 ITU-T X.1631 (07/2015) Last line."},
		{"no page number", "ITU-T X.1631 (07/2015)\n", "ITU-T X.1631 (07/2015)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := []string{"5   Root\n\n5.1   Branch\n" + tc.tail, strings.Repeat(" ", 60) + "Annex A\n" + strings.Repeat(" ", 59) + "Annex B\n"}
			count := 2
			got, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
			if err != nil {
				t.Fatal(err)
			}
			if got.Nodes[1].Body != tc.body {
				t.Fatalf("unexpected body: %q", got.Nodes[1].Body)
			}
		})
	}
}

func TestISO27017_SectionLabelBoundedAllocation(t *testing.T) {
	long := "Other information " + strings.Repeat("x ", 50000)
	line := isoLine{raw: long, text: long}
	annex := false
	classifier := iso27017Body{annex: &annex}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 4; i++ {
		_, _ = classifier.classify(line, nil, isoHeading{}, false)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4*65536 {
		t.Fatalf("section labels allocate %d bytes", allocated)
	}
}

func TestISO27017_TableHeaderBoundedAllocation(t *testing.T) {
	long := strings.Repeat("x  ", 50000)
	anchor := isoLine{raw: "Implementation guidance for cloud services", text: "Implementation guidance for cloud services"}
	next := []isoLine{{raw: " " + long, text: long}}
	annex := false
	classifier := iso27017Body{annex: &annex}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 4; i++ {
		_, _ = classifier.classify(anchor, next, isoHeading{}, false)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4*65536 {
		t.Fatalf("table headers allocate %d bytes", allocated)
	}
}
