package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestISO27002_AttributeTableRejectsUnexpectedLabels(t *testing.T) {
	for _, item := range []struct{ name, bad string }{{"section", "Guidance"}, {"indented section", "   Guidance"}, {"indented control", "   Control"}, {"repeated header", inventedTable}} {
		t.Run(item.name, func(t *testing.T) {
			pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\n#tag\n" + item.bad + "\nControl\nStatement.\n                 Annex A\n(informative)"}
			layout := iso27002Layout{sourceSHA: syntheticSHA(pages), annexColumn: 17}
			count := 2
			_, err := parseISO27002Pages(pages, layout.sourceSHA, &count, layout)
			if err == nil || err.Error() != "invalid attribute table" {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestISOReader_FooterCluster(t *testing.T) {
	pages := []string{
		"Header\nStart\n42\nNext\nfooter marker\nEnd",
		"Header\nStart\nfooter marker 12\n12\nEnd",
		"Header\nStart\n12\nfooter marker 12\nEnd",
		"Header\nStart\n42\nEnd",
	}
	reader := isoReader{pages: pages, header: "Header", footer: "footer marker"}
	var lines []string
	for {
		line, ok := reader.next()
		if !ok {
			break
		}
		lines = append(lines, line.text)
	}
	want := []string{"Start", "42", "Next", "End", "Start", "End", "Start", "End", "Start", "42", "End"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines: %v", lines)
	}
}

func TestISO27002_ProfileTableRange(t *testing.T) {
	pages := []string{"5 Domain\n5.1 Title\n" + inventedTable + "\nControl\nStatement.\nGuidance\nProse  has double spaces\nRow A  one\nRow B  two\nBack to prose\n                 Annex A\n(informative)"}
	layout := iso27002Layout{sourceSHA: syntheticSHA(pages), annexColumn: 17, tables: []isoTableRange{{page: 1, first: 8, last: 9}}}
	count := 2
	got, err := parseISO27002Pages(pages, layout.sourceSHA, &count, layout)
	if err != nil {
		t.Fatal(err)
	}
	want := "Control\nStatement.\n\nGuidance\nProse has double spaces\nRow A one\nRow B two\nBack to prose"
	if got.Nodes[1].Body != want {
		t.Fatalf("body: %q", got.Nodes[1].Body)
	}
}

func TestISOWalker_ProfileLineActions(t *testing.T) {
	profile := isoProfile{
		start: func(line isoLine, next []isoLine) bool { return line.text == "START" },
		stop:  func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			return isoHeading{citation: "X", kind: "clause", direct: true}, line.text == "ROOT"
		},
		parent: func(h isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			return -1, nil
		},
		bodyLine: func(line isoLine) isoLineAction {
			if line.text == "DROP" {
				return isoDrop
			}
			if line.text == "BREAK" {
				return isoKeepBreak
			}
			return isoKeep
		},
	}
	nodes, err := walkISO([]string{"START\nROOT\nFirst\nDROP\nsecond\nBREAK\nlast\nSTOP"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(nodes[0].Body, "First second\nBREAK\nlast") {
		t.Fatalf("body: %q", nodes[0].Body)
	}
}
