package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestISO27017_RegionsAndParents(t *testing.T) {
	pages := []string{
		"5   Index ........ 1\n",
		"5   Root\n\n5.1   Branch\n\n5.1.1   Leaf\nText.\n\n" + strings.Repeat(" ", 60) + "Annex A\nPreface.\n\nCLD.1.1 Root\n\nCLD.1.1.1 Child\nControl\nStatement.\n\n" + strings.Repeat(" ", 59) + "Annex B\n\nCLD.1.1.2 Ignored\nControl\nIgnored.\n",
	}
	count := 5
	got, err := ParsePages("iso27017-2015-pdf", pages, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
	if err != nil {
		t.Fatal(err)
	}
	citations := make([]string, len(got.Nodes))
	parents := make([]int, len(got.Nodes))
	for i, node := range got.Nodes {
		citations[i], parents[i] = node.Citation, node.Parent
		if node.Kind != "control" {
			t.Fatalf("node %d has wrong kind", i)
		}
	}
	if !reflect.DeepEqual(citations, []string{"5", "5.1", "5.1.1", "CLD.1.1", "CLD.1.1.1"}) || !reflect.DeepEqual(parents, []int{-1, 0, 1, -1, 3}) {
		t.Fatalf("unexpected tree: %v %v", citations, parents)
	}
}

func TestISO27017_InvalidRegionsAndParents(t *testing.T) {
	start := "5   Root\n\n5.1   Branch\n"
	annexA := strings.Repeat(" ", 60) + "Annex A\n"
	annexB := strings.Repeat(" ", 59) + "Annex B\n"
	cases := []struct{ name, page, want string }{
		{"missing start", "6   Root\n" + annexA + annexB, "missing required region"},
		{"missing stop", start + annexA, "missing required region"},
		{"missing main parent", start + "\n5.2.1   Leaf\n" + annexA + annexB, "missing control parent"},
		{"missing annex parent", start + annexA + "\nCLD.2.1.1 Child\nControl\nStatement\n" + annexB, "missing control parent"},
		{"duplicate", start + "\n5.1   Duplicate\n" + annexA + annexB, "invalid or duplicate citations"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := 2
			_, err := ParsePages("iso27017-2015-pdf", []string{tc.page + "SENTINEL"}, "", "", "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a", &count)
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "SENTINEL") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestISO27017_WrongFingerprint(t *testing.T) {
	count := 1
	_, err := ParsePages("iso27017-2015-pdf", []string{"SENTINEL"}, "", "", "wrong", &count)
	if err == nil || err.Error() != "source fingerprint mismatch" {
		t.Fatalf("unexpected error: %v", err)
	}
}
