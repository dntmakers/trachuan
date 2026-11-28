package parse

import (
	"fmt"
	"regexp"
	"strings"
)

func parseISO27017Pages(pages []string, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != "baa45e80a596e7eb929fd3450dcf1ea6f96ec07f6cca12aa0efe552c2fe0bf5a" {
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
	nodes, err := parseISO27017(pages)
	return finish(nodes, err, expected)
}

func parseISO27017(pages []string) ([]Node, error) {
	mainRE := regexp.MustCompile(`^((?:[5-9]|1[0-8])(?:\.[0-9]+){0,2}) {3,}([A-Z].*)$`)
	annexRE := regexp.MustCompile(`^(CLD\.[0-9]+\.[0-9]+(?:\.[0-9]+)?) +([A-Z].*)$`)
	annex := false
	body := iso27017Body{annex: &annex}
	footer := regexp.MustCompile(`^(?:[0-9]+\s+(?:Rec\.\s+)?ITU-T\s+X\.1631\s+\(07/2015\)|(?:Rec\.\s+)?ITU-T\s+X\.1631\s+\(07/2015\)\s+[0-9]+)$`)
	match := func(line isoLine, re *regexp.Regexp) []string {
		if !isoFlush(line) || strings.Contains(line.text, "....") || strings.HasSuffix(line.text, ".") {
			return nil
		}
		return re.FindStringSubmatch(line.raw)
	}
	heading := func(line isoLine, next []isoLine) (isoHeading, bool) {
		if !line.blankBefore && !line.firstContent {
			return isoHeading{}, false
		}
		re := mainRE
		if annex {
			re = annexRE
		}
		m := match(line, re)
		if m == nil {
			return isoHeading{}, false
		}
		if annex && strings.Count(m[1], ".") == 3 && (len(next) == 0 || !isoFlush(next[0]) || next[0].text != "Control") {
			return isoHeading{}, false
		}
		return isoHeading{citation: m[1], title: strings.Join(strings.Fields(m[2]), " "), kind: "control", direct: true}, true
	}
	profile := isoProfile{
		header: "ISO/IEC 27017:2015 (E)", lookahead: 1,
		footerLast: func(line isoLine) bool { return footer.MatchString(line.text) },
		joinRules: func(isoHeading) isoJoinRules {
			return isoJoinRules{markers: []string{"–", "—", "•"}, letterItems: true, digitItems: true, paragraphs: []string{"NOTE"}, blankParagraph: true, pageParagraph: true, labelParagraphEach: true}
		},
		classify:     body.classify,
		validateNode: func(Node) error { return body.finish() },
		start: func(line isoLine, next []isoLine) bool {
			m := match(line, mainRE)
			if m == nil || m[1] != "5" || len(next) == 0 {
				return false
			}
			n := match(next[0], mainRE)
			return n != nil && n[1] == "5.1"
		},
		stop: func(line isoLine, _ []isoLine) bool {
			indent := len(line.raw) - len(strings.TrimLeft(line.raw, " "))
			if line.text == "Annex A" && indent == 60 {
				annex = true
			}
			return line.text == "Annex B" && indent == 59 && annex
		},
		heading: heading,
		parent: func(h isoHeading, _ int, _ string, lookup func(string) (int, bool)) (int, error) {
			if (!strings.HasPrefix(h.citation, "CLD.") && !strings.Contains(h.citation, ".")) || (strings.HasPrefix(h.citation, "CLD.") && strings.Count(h.citation, ".") == 2) {
				return -1, nil
			}
			parent, ok := lookup(h.citation[:strings.LastIndexByte(h.citation, '.')])
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
	hasChild := make([]bool, len(nodes))
	for _, node := range nodes {
		if node.Parent >= 0 {
			hasChild[node.Parent] = true
		}
	}
	for i, node := range nodes {
		if node.Body == "" && !hasChild[i] {
			return nil, fmt.Errorf("missing control body")
		}
	}
	return nodes, nil
}
