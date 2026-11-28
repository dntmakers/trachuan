package parse

import (
	"fmt"
	"regexp"
	"strings"
)

func parseISO27001Pages(pages []string, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != "e270769945c630894a21f64fd018bcdc586a82281edcd4c29a5d864ddf371b54" {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	const maxPageBytes = 50_000_000
	total := 0
	for _, page := range pages {
		if len(page) > maxPageBytes-total {
			return nil, fmt.Errorf("page text limit exceeded")
		}
		total += len(page)
	}
	nodes, err := parseISO27001(pages)
	return finish(nodes, err, expected)
}

func parseISO27001(pages []string) ([]Node, error) {
	mainRE := regexp.MustCompile(`^([4-9]|10)(\.[1-9][0-9]*){0,2}(?:\t\x{200b}| {3,6})([A-Z].*)$`)
	annexRE := regexp.MustCompile(`^([5-8](?:\.[1-9][0-9]*)?)( +)(\S.*)$`)
	captionRE := regexp.MustCompile(`^Table A\.1\s+[—–-]\s+\S`)
	continuationRE := regexp.MustCompile(`^Table A\.1\s*\([^)]*\)$`)
	annex, table, openingCaption := false, false, false
	clause := func(line isoLine) (isoHeading, bool) {
		if !line.blankBefore && !line.firstContent {
			return isoHeading{}, false
		}
		m := mainRE.FindStringSubmatch(line.raw)
		if m == nil || strings.Contains(m[3], "....") || strings.HasSuffix(strings.TrimSpace(m[3]), ".") {
			return isoHeading{}, false
		}
		citation := strings.SplitN(line.raw, "\t", 2)[0]
		citation = strings.Fields(citation)[0]
		return isoHeading{citation: citation, title: strings.TrimSpace(strings.ReplaceAll(m[3], "\u200b", "")), kind: "clause", direct: true}, true
	}
	profile := isoProfile{
		header: "ISO/IEC 27001:2022(E)", footer: "© ISO/IEC 2022 – All rights reserved", headerSuffix: true, lookahead: 1,
		start: func(line isoLine, next []isoLine) bool {
			h, ok := clause(line)
			if !ok || h.citation != "4" || len(next) == 0 {
				return false
			}
			n, ok := clause(next[0])
			return ok && n.citation == "4.1"
		},
		stop: func(line isoLine, next []isoLine) bool {
			openingCaption = false
			if !annex && !isoFlush(line) && line.text == "Annex A" {
				annex = true
				return false
			}
			if annex && !table && !isoFlush(line) && captionRE.MatchString(line.text) {
				table = true
				openingCaption = true
				return false
			}
			return annex && !isoFlush(line) && line.text == "Bibliography"
		},
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			if !annex {
				return clause(line)
			}
			if !table {
				return isoHeading{}, false
			}
			m := annexRE.FindStringSubmatch(line.raw)
			if m == nil {
				return isoHeading{}, false
			}
			column := len(m[1]) + len(m[2])
			if column != 7 && column != 10 {
				return isoHeading{}, false
			}
			kind, title := "domain", strings.TrimSpace(m[3])
			if strings.Contains(m[1], ".") {
				kind = "annex-control"
				pos := strings.LastIndex(line.raw, "Control")
				if pos < 35 || pos > 45 || line.raw[pos-1] != ' ' || strings.TrimSpace(line.raw[pos:]) != "Control" {
					return isoHeading{}, false
				}
				title = strings.TrimSpace(line.raw[column:pos])
			}
			return isoHeading{citation: "A." + m[1], title: title, kind: kind, titleColumn: column, direct: true}, true
		},
		classify: func(line isoLine, next []isoLine, h isoHeading, heading bool) (isoLineAction, error) {
			if heading {
				return isoKeep, nil
			}
			if !annex {
				return isoKeep, nil
			}
			if !table || openingCaption || (!isoFlush(line) && line.firstContent && continuationRE.MatchString(line.text)) || line.text == "Annex A" {
				return isoDrop, nil
			}
			if h.kind != "annex-control" {
				return isoDrop, fmt.Errorf("invalid annex continuation")
			}
			column := len(line.raw) - len(strings.TrimLeft(line.raw, " "))
			if column == h.titleColumn {
				return isoTitle, nil
			}
			if column >= 35 && column <= 45 {
				return isoKeep, nil
			}
			return isoDrop, fmt.Errorf("invalid annex continuation")
		},
		joinRules: func(h isoHeading) isoJoinRules {
			if h.kind == "annex-control" {
				return isoJoinRules{}
			}
			return isoJoinRules{markers: []string{"—"}, letterItems: true, digitItems: true, paragraphs: []string{"NOTE"}, blankParagraph: true, pageParagraph: true}
		},
		validateNode: func(node Node) error {
			if node.Kind == "annex-control" && (node.PublisherTitle == "" || node.Body == "") {
				return fmt.Errorf("missing annex statement")
			}
			return nil
		},
		parent: func(h isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			if h.kind == "domain" || (h.kind == "clause" && !strings.Contains(h.citation, ".")) {
				return -1, nil
			}
			idx := strings.LastIndex(h.citation, ".")
			if root < 0 || !strings.HasPrefix(h.citation, rootCitation+".") || idx < 0 {
				return -1, fmt.Errorf("missing parent")
			}
			parent, ok := lookup(h.citation[:idx])
			if !ok {
				return -1, fmt.Errorf("missing parent")
			}
			return parent, nil
		},
	}
	nodes, err := walkISO(pages, profile)
	if err != nil {
		return nil, err
	}
	if !table {
		return nil, fmt.Errorf("missing required region")
	}
	hasChild := make([]bool, len(nodes))
	for _, node := range nodes {
		if node.Parent >= 0 {
			hasChild[node.Parent] = true
		}
	}
	for i, node := range nodes {
		if node.Kind == "clause" && node.Body == "" && !hasChild[i] {
			return nil, fmt.Errorf("missing clause body")
		}
	}
	return nodes, nil
}
