package parse

import (
	"strings"
	"testing"
)

func TestISO27002_Fingerprint(t *testing.T) {
	count := 2
	_, err := ParsePages("iso27002-2022-pdf", []string{"SENTINEL"}, "", "", "bad", &count)
	if err == nil || err.Error() != "source fingerprint mismatch" {
		t.Fatalf("error %v", err)
	}
	profile := iso27002Layout{annexColumn: 17}
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nBody.\n                 Annex A\n(informative)"}
	profile.sourceSHA = syntheticSHA(pages)
	if _, err := parseISO27002Pages(pages, "different-sha", &count, profile); err == nil || err.Error() != "source fingerprint mismatch" {
		t.Fatalf("error %v", err)
	}
	if _, err := parseISO27002Pages(pages, profile.sourceSHA, &count, profile); err != nil {
		t.Fatal(err)
	}
}

func TestISO27002_AnnexAndHeadingPositions(t *testing.T) {
	profile := iso27002Layout{annexColumn: 17}
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nText.\nGuidance\n                 Annex A\nNot a confirmation.\n                  Annex A\n(informative)\n 5.2 Indented citation\nMore text.\n                 Annex A\n(informative)"}
	profile.sourceSHA = syntheticSHA(pages)
	count := 2
	got, err := parseISO27002Pages(pages, profile.sourceSHA, &count, profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got.Nodes[1].Body, "Annex A") != 2 || !strings.Contains(got.Nodes[1].Body, "5.2 Indented citation") {
		t.Fatal("lost body text")
	}
}

func TestISO27002_RejectsExtraAttributeColumn(t *testing.T) {
	profile := iso27002Layout{annexColumn: 17}
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "  Extra\nControl\nText.\n                 Annex A\n(informative)"}
	profile.sourceSHA = syntheticSHA(pages)
	count := 2
	_, err := parseISO27002Pages(pages, profile.sourceSHA, &count, profile)
	if err == nil || err.Error() != "missing required region" {
		t.Fatalf("error %v", err)
	}
}

func TestISO27002_DuplicateBeforeBadBody(t *testing.T) {
	profile := iso27002Layout{annexColumn: 17}
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nText.\n5.1 Duplicate\n" + inventedTable + "\n                 Annex A\n(informative)"}
	profile.sourceSHA = syntheticSHA(pages)
	count := 2
	_, err := parseISO27002Pages(pages, profile.sourceSHA, &count, profile)
	if err == nil || err.Error() != "invalid or duplicate citations" {
		t.Fatalf("error %v", err)
	}
}
