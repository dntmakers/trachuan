package parse

import (
	"fmt"
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

func TestISOReader_LineBoundaries(t *testing.T) {
	reader := isoReader{pages: []string{"label EDITION\nFirst\n\u200b\nSecond", "EDITION\nThird"}, header: "EDITION", headerSuffix: true}
	first, ok := reader.next()
	if !ok || !first.firstContent || !first.blankBefore || first.text != "First" {
		t.Fatal("first content boundary")
	}
	second, ok := reader.next()
	if !ok || second.firstContent || !second.blankBefore || second.text != "Second" {
		t.Fatal("blank boundary")
	}
	third, ok := reader.next()
	if !ok || !third.firstContent || third.text != "Third" {
		t.Fatal("page boundary")
	}
}

func TestISOJoin_Prose(t *testing.T) {
	var body isoBody
	for _, line := range []isoLine{
		{text: "First.", page: 1},
		{text: "Same paragraph.", page: 1},
		{text: "New paragraph:", page: 1, blankBefore: true},
		{text: "a) Item", page: 1, blankBefore: true},
		{text: "continued;", page: 1},
		{text: "Next page.", page: 2, blankBefore: true},
		{text: "NOTE Example.", page: 2},
	} {
		body.addJoined(line, isoKeep, isoJoinRules{markers: []string{"—"}, letterItems: true, digitItems: true, paragraphs: []string{"NOTE"}, blankParagraph: true, pageParagraph: true})
	}
	want := "First. Same paragraph.\n\nNew paragraph:\n\na) Item continued;\n\nNext page.\n\nNOTE Example."
	if body.text.String() != want {
		t.Fatalf("unexpected body: %q", body.text.String())
	}
}

func TestISOJoin_ProfileBehaviors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []isoLine
		rules isoJoinRules
		want  string
	}{
		{"en dash list", []isoLine{{text: "First.", page: 1}, {text: "– Item", page: 1}}, isoJoinRules{markers: []string{"–"}}, "First.\n– Item"},
		{"example paragraph", []isoLine{{text: "First.", page: 1}, {text: "EXAMPLE A sample.", page: 1}}, isoJoinRules{paragraphs: []string{"NOTE", "EXAMPLE"}}, "First.\n\nEXAMPLE A sample."},
		{"standalone label", []isoLine{{text: "Control", raw: "Control", page: 1}, {text: "Statement.", page: 1}}, isoJoinRules{label: func(line isoLine) bool { return line.text == "Control" && isoFlush(line) }, labelParagraph: true}, "Control\nStatement."},
		{"blank after dropped page lines", []isoLine{{text: "Previous", page: 1}, {text: "New paragraph", page: 2, blankBefore: true, firstContent: false}}, isoJoinRules{blankParagraph: true}, "Previous\n\nNew paragraph"},
		{"default punctuation", []isoLine{{text: "First.", page: 1}, {text: "Second:", page: 1}, {text: "Third;", page: 1}, {text: "Last", page: 1}}, isoDefaultJoinRules(), "First.\nSecond:\nThird;\nLast"},
		{"cell joining", []isoLine{{text: "First.", page: 1}, {text: "Second", page: 2, blankBefore: true}}, isoJoinRules{}, "First. Second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body isoBody
			for _, line := range tc.lines {
				body.addJoined(line, isoKeep, tc.rules)
			}
			if body.text.String() != tc.want {
				t.Fatalf("got %q want %q", body.text.String(), tc.want)
			}
		})
	}
}

func TestISOWalker_ClassifierHeadingEvents(t *testing.T) {
	events := 0
	profile := isoProfile{
		lookahead: 1,
		start:     func(line isoLine, next []isoLine) bool { return line.text == "START" },
		stop:      func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			return isoHeading{citation: "A", direct: true}, line.text == "HEADING"
		},
		parent: func(h isoHeading, root int, citation string, lookup func(string) (int, bool)) (int, error) {
			return -1, nil
		},
		classify: func(line isoLine, next []isoLine, h isoHeading, heading bool) (isoLineAction, error) {
			if heading && line.text == "HEADING" && h.citation == "A" && len(next) == 1 && next[0].text == "Body" {
				events++
			}
			if !heading && line.text == "Body" && len(next) == 1 && next[0].text == "STOP" {
				events++
			}
			return isoKeep, nil
		},
	}
	_, err := walkISO([]string{"START\nHEADING\nBody\nSTOP"}, profile)
	if err != nil || events != 2 {
		t.Fatalf("heading events: %d; error: %v", events, err)
	}
}

func TestISOJoin_ExplicitBreaks(t *testing.T) {
	var body isoBody
	body.addJoined(isoLine{text: "First", page: 1}, isoKeep, isoJoinRules{})
	body.addJoined(isoLine{text: "Row", page: 1}, isoKeepBreak, isoJoinRules{})
	body.addJoined(isoLine{text: "After", page: 1}, isoKeep, isoJoinRules{})
	body.addJoined(isoLine{text: "Label", page: 1}, isoKeepLabel, isoJoinRules{labelParagraph: true})
	body.addJoined(isoLine{text: "Statement", page: 1}, isoKeep, isoJoinRules{})
	if got := body.text.String(); got != "First\nRow\nAfter\n\nLabel\nStatement" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestISOWalker_ClassifierTableBoundary(t *testing.T) {
	inTable := false
	profile := isoProfile{
		lookahead: 1,
		start:     func(line isoLine, next []isoLine) bool { return line.text == "ROOT" },
		stop:      func(line isoLine, next []isoLine) bool { return line.text == "STOP" },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			return isoHeading{citation: line.text, direct: true}, line.text == "ROOT" || line.text == "NEXT"
		},
		parent: func(h isoHeading, root int, citation string, lookup func(string) (int, bool)) (int, error) {
			return -1, nil
		},
		classify: func(line isoLine, next []isoLine, h isoHeading, heading bool) (isoLineAction, error) {
			if heading {
				inTable = false
				return isoKeep, nil
			}
			if line.text == "TABLE" {
				if len(next) != 1 || next[0].text != "PAIRED COLUMNS" {
					return isoDrop, fmt.Errorf("missing table header")
				}
				inTable = true
				return isoDrop, nil
			}
			if inTable {
				return isoDrop, nil
			}
			return isoKeep, nil
		},
	}
	nodes, err := walkISO([]string{"ROOT\nBefore.\nTABLE\nPAIRED COLUMNS\nHidden.\nNEXT\nAfter.\nSTOP"}, profile)
	if err != nil || len(nodes) != 2 || nodes[0].Body != "Before." || nodes[1].Body != "After." {
		t.Fatalf("unexpected table boundary: %v", err)
	}
}

func TestISOJoin_EditionProfiles(t *testing.T) {
	label := func(line isoLine) bool {
		return isoFlush(line) && (line.text == "Control" || strings.HasPrefix(line.text, "Other information"))
	}
	for _, tc := range []struct {
		name  string
		rules isoJoinRules
		lines []isoLine
		want  string
	}{
		{"27017", isoJoinRules{markers: []string{"–"}, letterItems: true, digitItems: true, paragraphs: []string{"NOTE"}, label: label, blankParagraph: true, pageParagraph: true, labelParagraphEach: true}, []isoLine{{text: "Sentence.", page: 1}, {text: "Continuation.", page: 2, firstContent: true}, {text: "– Item", page: 2}, {text: "NOTE A note", page: 2}, {raw: "Other information for cloud services", text: "Other information for cloud services", page: 2}, {text: "Details.", page: 2}}, "Sentence.\n\nContinuation.\n– Item\n\nNOTE A note\n\nOther information for cloud services\nDetails."},
		{"27018", isoJoinRules{markers: []string{"—"}, letterItems: true, paragraphs: []string{"NOTE", "EXAMPLE"}, label: label, blankParagraph: true, pageParagraph: true, labelParagraph: true}, []isoLine{{text: "Previous.", page: 1}, {text: "EXAMPLE A sample.", page: 1}, {raw: "Control", text: "Control", page: 1}, {text: "Statement.", page: 1}, {raw: "Other information for cloud PII", text: "Other information for cloud PII", page: 1}, {text: "Details.", page: 1}}, "Previous.\n\nEXAMPLE A sample.\n\nControl\nStatement.\n\nOther information for cloud PII\nDetails."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body isoBody
			for _, line := range tc.lines {
				body.addJoined(line, isoKeep, tc.rules)
			}
			if got := body.text.String(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestISOJoin_LetterItemsWithoutNumericItems(t *testing.T) {
	var body isoBody
	rules := isoJoinRules{letterItems: true}
	for _, line := range []string{"Intro", "a) Letter item", "1) Numeric continuation"} {
		body.addJoined(isoLine{text: line, page: 1}, isoKeep, rules)
	}
	if got := body.text.String(); got != "Intro\na) Letter item 1) Numeric continuation" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestISOJoin_ConsecutiveLabelParagraphs(t *testing.T) {
	var body isoBody
	rules := isoJoinRules{labelParagraphEach: true}
	body.addJoined(isoLine{text: "Introduction", page: 1}, isoKeep, rules)
	body.addJoined(isoLine{text: "Label one", page: 1}, isoKeepLabel, rules)
	body.addJoined(isoLine{text: "Label two", page: 1}, isoKeepLabel, rules)
	body.addJoined(isoLine{text: "Text", page: 1}, isoKeep, rules)
	if got := body.text.String(); got != "Introduction\n\nLabel one\n\nLabel two\nText" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestISOJoin_IndependentItemOptions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules isoJoinRules
		want  string
	}{
		{"neither", isoJoinRules{}, "Intro a) Letter 1) Number"},
		{"letters", isoJoinRules{letterItems: true}, "Intro\na) Letter 1) Number"},
		{"numbers", isoJoinRules{digitItems: true}, "Intro a) Letter\n1) Number"},
		{"both", isoJoinRules{letterItems: true, digitItems: true}, "Intro\na) Letter\n1) Number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body isoBody
			for _, line := range []string{"Intro", "a) Letter", "1) Number"} {
				body.addJoined(isoLine{text: line, page: 1}, isoKeep, tc.rules)
			}
			if got := body.text.String(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestISOJoin_ConsecutiveLabelsOneParagraph(t *testing.T) {
	var body isoBody
	rules := isoJoinRules{labelParagraph: true}
	body.addJoined(isoLine{text: "Introduction", page: 1}, isoKeep, rules)
	body.addJoined(isoLine{text: "Label one", page: 1}, isoKeepLabel, rules)
	body.addJoined(isoLine{text: "Label two", page: 1}, isoKeepLabel, rules)
	if got := body.text.String(); got != "Introduction\n\nLabel one\nLabel two" {
		t.Fatalf("unexpected body: %q", got)
	}
}
