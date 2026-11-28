package parse

import (
	"strings"
	"testing"
)

func TestISOWalker_Profile(t *testing.T) {
	profile := isoProfile{
		lookahead: 1,
		start:     func(line isoLine, next []isoLine) bool { return line.text == "START" },
		stop:      func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			if line.text != "ROOT" {
				return isoHeading{}, false
			}
			return isoHeading{citation: "X", title: "Invented", kind: "control"}, true
		},
		parent: func(heading isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			return -1, nil
		},
		sections: []isoSection{{label: "Statement", required: true}, {label: "Details"}},
	}
	nodes, err := walkISO([]string{"START\nROOT\nStatement\nFirst line\nDetails\nSecond line.\nSTOP"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Body != "Statement\nFirst line\n\nDetails\nSecond line." {
		t.Fatal("profile behavior")
	}
	_, err = walkISO([]string{"START\nROOT\nDetails\nText.\nStatement\nText.\nSTOP"}, profile)
	if err == nil {
		t.Fatal("accepted out-of-order sections")
	}
}

func TestISOWalker_DirectBody(t *testing.T) {
	profile := isoProfile{
		start: func(line isoLine, next []isoLine) bool { return line.text == "START" },
		stop:  func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			return isoHeading{citation: "X", kind: "clause", direct: true}, line.text == "ROOT"
		},
		parent: func(heading isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			return -1, nil
		},
	}
	nodes, err := walkISO([]string{"START\nROOT\nFirst\nsecond.\nSTOP"}, profile)
	if err != nil || len(nodes) != 1 || nodes[0].Body != "First second." {
		t.Fatal("direct body")
	}
}

func TestISOWalker_LongBody(t *testing.T) {
	count := 2
	text := "5 Domain\n5.1 Title\n" + inventedTable + "\nControl\n" + strings.Repeat("word\n", 20000) + "                 Annex A"
	got, err := parseSyntheticPages("iso27002-2022-pdf", []string{text}, "", "", &count)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[1].Body != "Control\n"+strings.TrimSuffix(strings.Repeat("word ", 20000), " ") {
		t.Fatal("long body differs")
	}
}
