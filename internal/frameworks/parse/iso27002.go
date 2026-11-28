package parse

import (
	"fmt"
	"regexp"
	"strings"
)

type isoTableRange struct {
	page, first, last int
}

type iso27002Layout struct {
	sourceSHA   string
	annexColumn int
	tables      []isoTableRange
}

func parseISO27002(pages []string, layout iso27002Layout) ([]Node, error) {
	domainRE := regexp.MustCompile(`^([5-8]) +([A-Z].*)$`)
	controlRE := regexp.MustCompile(`^([5-8])\.([0-9]{1,2}) +(\S.*)$`)
	columnsRE := regexp.MustCompile(` {2,}`)
	attribute := func(line isoLine) bool {
		if len(line.raw)-len(strings.TrimLeft(line.raw, " ")) < 2 {
			return false
		}
		columns := columnsRE.Split(strings.TrimSpace(line.raw), 6)
		return len(columns) == 5 && columns[0] == "Control type"
	}
	annex := func(line isoLine, next []isoLine) bool {
		return line.text == "Annex A" && len(line.raw)-len(strings.TrimLeft(line.raw, " ")) == layout.annexColumn && len(next) > 0 && next[0].text == "(informative)"
	}
	controlAt := func(line isoLine, next []isoLine) (isoHeading, bool) {
		if !isoFlush(line) {
			return isoHeading{}, false
		}
		match := controlRE.FindStringSubmatch(line.text)
		if match == nil || strings.Contains(line.text, "....") {
			return isoHeading{}, false
		}
		for i, candidate := range next {
			if i >= 5 {
				break
			}
			if attribute(candidate) {
				title := match[3]
				for _, continuation := range next[:i] {
					title += " " + continuation.text
				}
				return isoHeading{citation: match[1] + "." + match[2], title: strings.Join(strings.Fields(title), " "), kind: "control", skip: i + 1, table: true}, true
			}
			if annex(candidate, next[i+1:]) || (isoFlush(candidate) && !strings.Contains(candidate.text, "....") && (controlRE.MatchString(candidate.raw) || domainRE.MatchString(candidate.raw))) {
				break
			}
		}
		return isoHeading{}, false
	}
	heading := func(line isoLine, next []isoLine) (isoHeading, bool) {
		if isoFlush(line) {
			m := domainRE.FindStringSubmatch(line.text)
			if m != nil && !strings.Contains(line.text, "....") && len(next) > 0 {
				control, ok := controlAt(next[0], next[1:])
				if ok && control.citation == m[1]+".1" {
					return isoHeading{citation: m[1], title: strings.Join(strings.Fields(m[2]), " "), kind: "domain", direct: true}, true
				}
			}
		}
		return controlAt(line, next)
	}
	profile := isoProfile{
		header: "ISO/IEC 27002:2022(E)", footer: "© ISO/IEC 2022 – All rights reserved", lookahead: 6,
		start: func(line isoLine, next []isoLine) bool {
			h, ok := heading(line, next)
			return ok && h.kind == "domain" && h.citation == "5"
		},
		stop:    annex,
		heading: heading,
		parent: func(h isoHeading, root int, rootCitation string, lookup func(string) (int, bool)) (int, error) {
			if h.kind == "domain" {
				return -1, nil
			}
			if root >= 0 && strings.HasPrefix(h.citation, rootCitation+".") {
				return root, nil
			}
			return -1, fmt.Errorf("missing control parent")
		},
		sections:  []isoSection{{label: "Control", required: true}, {label: "Purpose"}, {label: "Guidance"}, {label: "Other information"}},
		tableEnd:  "Control",
		attribute: attribute,
		bodyLine: func(line isoLine) isoLineAction {
			for _, table := range layout.tables {
				if line.page == table.page && line.line >= table.first && line.line <= table.last {
					return isoKeepBreak
				}
			}
			return isoKeep
		},
	}
	return walkISO(pages, profile)
}
