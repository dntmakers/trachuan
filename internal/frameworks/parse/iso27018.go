package parse

import (
	"fmt"
	"regexp"
	"strings"
)

func parseISO27018Pages(pages []string, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != "83e63bb508e90238d101d964d6b0d00eabe1e564e32a697181a7dc230ccf7cdf" {
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
	nodes, err := parseISO27018(pages)
	return finish(nodes, err, expected)
}

func parseISO27018(pages []string) ([]Node, error) {
	mainRE := regexp.MustCompile(`^((?:[5-9]|1[0-8])(?:\.[1-9][0-9]*){0,2}) +([A-Z].*)$`)
	annexRE := regexp.MustCompile(`^(A\.[1-9][0-9]*(?:\.[1-9][0-9]*)?) +([A-Z].*)$`)
	atColumn := func(line isoLine, text string, column int) bool {
		return line.text == text && len(line.raw)-len(strings.TrimLeft(line.raw, " ")) == column
	}
	match := func(line isoLine, re *regexp.Regexp) []string {
		if !isoFlush(line) || strings.Contains(line.text, "....") || strings.HasSuffix(line.text, ".") {
			return nil
		}
		return re.FindStringSubmatch(line.text)
	}
	annex, annexReady := false, false
	lastDomainPage, lastDomainLine := 0, 0
	profile := isoProfile{
		header: "ISO/IEC 27018:2019(E)", footer: "© ISO/IEC 2019 – All rights reserved", lookahead: 1,
		start: func(line isoLine, next []isoLine) bool {
			m := match(line, mainRE)
			if m == nil || m[1] != "5" || len(next) == 0 {
				return false
			}
			n := match(next[0], mainRE)
			return n != nil && n[1] == "5.1"
		},
		stop: func(line isoLine, _ []isoLine) bool { return annex && atColumn(line, "Bibliography", 43) },
		heading: func(line isoLine, next []isoLine) (isoHeading, bool) {
			if atColumn(line, "Annex A", 47) {
				annex = true
				return isoHeading{}, false
			}
			if !annex {
				m := match(line, mainRE)
				if m != nil && (line.blankBefore || line.firstContent) {
					return isoHeading{citation: m[1], title: m[2], kind: "control", direct: true}, true
				}
				return isoHeading{}, false
			}
			m := match(line, annexRE)
			if m == nil {
				return isoHeading{}, false
			}
			child := strings.Count(m[1], ".") == 2
			adjacent := line.page == lastDomainPage && line.line == lastDomainLine+1
			if !line.blankBefore && !line.firstContent && (!child || !adjacent) {
				return isoHeading{}, false
			}
			kind := "domain"
			if child {
				if len(next) == 0 || !isoFlush(next[0]) || next[0].text != "Control" {
					return isoHeading{}, false
				}
				kind = "annex-control"
			} else {
				lastDomainPage, lastDomainLine = line.page, line.line
				annexReady = true
			}
			return isoHeading{citation: m[1], title: m[2], kind: kind, direct: true}, true
		},
		bodyLine: func(line isoLine) isoLineAction {
			if annex && !annexReady {
				return isoDrop
			}
			return isoKeep
		},
		joinRules: func(isoHeading) isoJoinRules {
			return isoJoinRules{
				markers: []string{"—"}, letterItems: true, paragraphs: []string{"NOTE", "EXAMPLE"},
				blankParagraph: true, pageParagraph: true, labelParagraph: true,
				label: func(line isoLine) bool {
					return isoFlush(line) && (line.text == "Control" || (strings.HasPrefix(line.text, "Other information") && strings.Contains(line.text, "cloud") && strings.Contains(line.text, "PII")))
				},
			}
		},
		parent: func(h isoHeading, _ int, _ string, lookup func(string) (int, bool)) (int, error) {
			if h.kind == "domain" || (h.kind == "control" && !strings.Contains(h.citation, ".")) {
				return -1, nil
			}
			prefix := h.citation[:strings.LastIndexByte(h.citation, '.')]
			parent, ok := lookup(prefix)
			if !ok {
				return -1, fmt.Errorf("missing control parent")
			}
			return parent, nil
		},
	}
	nodes, err := walkISO(pages, profile)
	if err != nil {
		return nil, err
	}
	children := make([]bool, len(nodes))
	for _, node := range nodes {
		if node.Parent >= 0 {
			children[node.Parent] = true
		}
	}
	for i, node := range nodes {
		if node.Kind == "control" && node.Body == "" && !children[i] {
			return nil, fmt.Errorf("missing required body")
		}
		if node.Kind == "annex-control" {
			statement := strings.TrimPrefix(node.Body, "Control")
			statement, _, _ = strings.Cut(statement, "\nOther information")
			if !strings.HasPrefix(node.Body, "Control") || strings.TrimSpace(statement) == "" {
				return nil, fmt.Errorf("missing required body")
			}
		}
	}
	return nodes, nil
}
