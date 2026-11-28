package parse

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestISOWalker_ImmediateParentLookup(t *testing.T) {
	profile := isoProfile{
		start: func(line isoLine, next []isoLine) bool { return line.text == "START" },
		stop:  func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			if strings.HasPrefix(line.text, "H ") {
				return isoHeading{citation: strings.TrimPrefix(line.text, "H "), kind: "clause", direct: true}, true
			}
			return isoHeading{}, false
		},
		parent: func(h isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			at := strings.LastIndexByte(h.citation, '.')
			if at < 0 {
				return -1, nil
			}
			parent, ok := lookup(h.citation[:at])
			if !ok {
				return -1, fmt.Errorf("missing parent")
			}
			return parent, nil
		},
	}
	nodes, err := walkISO([]string{"START\nH 5\nH 5.1\nH 5.1.1\nH 5.2\nSTOP"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	var parents []int
	for _, n := range nodes {
		parents = append(parents, n.Parent)
	}
	if !reflect.DeepEqual(parents, []int{-1, 0, 1, 0}) {
		t.Fatalf("parents: %v", parents)
	}
	_, err = walkISO([]string{"START\nH 5\nH 5.1.1\nSTOP"}, profile)
	if err == nil || err.Error() != "missing parent" {
		t.Fatalf("error: %v", err)
	}
}

func TestISO27002_PageTextLimit(t *testing.T) {
	chunk := strings.Repeat(" ", 1_000_000)
	pages := make([]string, 50)
	for i := range pages {
		pages[i] = chunk
	}
	layout := iso27002Layout{sourceSHA: syntheticSHA([]string{"fixture"}), annexColumn: 17}
	count := 1
	_, err := parseISO27002Pages(pages, layout.sourceSHA, &count, layout)
	if err == nil || err.Error() != "missing required region" {
		t.Fatalf("at limit: %v", err)
	}
	pages = append(pages, "x")
	_, err = parseISO27002Pages(pages, layout.sourceSHA, &count, layout)
	if err == nil || err.Error() != "page text limit exceeded" {
		t.Fatalf("over limit: %v", err)
	}
}
