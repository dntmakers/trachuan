package parse

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tcvnLines struct {
	rest string
	prev string
	done bool
}

func (r *tcvnLines) next() (string, string, string, bool) {
	if r.done {
		return "", "", "", false
	}
	line, rest, found := strings.Cut(r.rest, "\n")
	previous := r.prev
	r.prev, r.rest, r.done = line, rest, !found
	next, _, _ := strings.Cut(rest, "\n")
	return previous, line, next, true
}

type tcvnBody struct {
	text strings.Builder
	gap  bool
}

func (b *tcvnBody) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		b.gap = true
		return
	}
	first, size := utf8.DecodeRuneInString(line)
	list := strings.HasPrefix(line, "-") || (unicode.IsLetter(first) && len(line) > size && line[size] == ')')
	if b.text.Len() > 0 {
		switch {
		case b.gap:
			b.text.WriteString("\n\n")
		case list:
			b.text.WriteByte('\n')
		default:
			b.text.WriteByte(' ')
		}
	}
	b.text.WriteString(line)
	b.gap = false
}

func parseTCVN11930Pages(pages []string, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != "9f46c1956803eb0c72c3709832c8442ea7df837c99ad5b8ce68804e583e93f71" {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	total := 0
	for _, page := range pages {
		if len(page) > 50_000_000-total {
			return nil, fmt.Errorf("page text limit exceeded")
		}
		total += len(page)
	}
	nodes, err := parseTCVN11930(pages)
	return finish(nodes, err, expected)
}

func parseTCVN11930(pages []string) ([]Node, error) {
	if len(pages) < 67 {
		return nil, fmt.Errorf("missing required region")
	}
	pattern := regexp.MustCompile(`^([5-9](?:\.[1-9][0-9]*){0,3}|A\.[1-9][0-9]*(?:\.[1-9][0-9]*)?) +([^ ].*)$`)
	var nodes []Node
	var body tcvnBody
	closeBody := func() {
		if len(nodes) > 0 {
			nodes[len(nodes)-1].Body = body.text.String()
		}
		body = tcvnBody{}
	}
	seen := make(map[string]int)
	duplicate := make(map[string]bool)
	pending, previous, before := "", "", ""
	started, annex, annexStarted, passageNonempty := false, false, false, false
	for pageIndex := 14; pageIndex < len(pages); pageIndex++ {
		first, last, contentFirst, contentLast := -1, -1, -1, -1
		var header, footer string
		reader := tcvnLines{rest: pages[pageIndex]}
		for i := 0; ; i++ {
			_, line, _, ok := reader.next()
			if !ok {
				break
			}
			if strings.TrimSpace(line) != "" {
				if first < 0 {
					first, header = i, line
				} else if contentFirst < 0 {
					contentFirst = i
				}
				contentLast, last, footer = last, i, line
			}
		}
		if first < 0 {
			continue
		}
		if strings.TrimSpace(header) != "TCVN 11930:2017" || !isoPageNumber(strings.TrimSpace(footer)) {
			return nil, fmt.Errorf("invalid page furniture")
		}
		body.gap = false
		reader = tcvnLines{rest: pages[pageIndex]}
		for i := 0; ; i++ {
			prevLine, raw, nextLine, ok := reader.next()
			if !ok || i >= last {
				break
			}
			if i <= first {
				continue
			}
			line := strings.TrimRight(raw, "\r")
			if pageIndex == 66 && i == 2 {
				if len(line)-len(strings.TrimLeft(line, " ")) != 46 || strings.TrimSpace(line) == "" {
					return nil, fmt.Errorf("missing required region")
				}
				annex = true
				continue
			}
			if pageIndex == 14 && i < 24 {
				continue
			}
			if pageIndex == 66 && i < 2 {
				continue
			}
			match := pattern.FindStringSubmatch(line)
			blankBefore := strings.TrimSpace(prevLine) == ""
			blankAfter := strings.TrimSpace(nextLine) == ""
			heading := match != nil && !strings.Contains(line, "....") && blankAfter
			if heading {
				citation := match[1]
				heading = (blankBefore || (annex && !annexStarted && citation == "A.1")) &&
					((!annex && !strings.HasPrefix(citation, "A.")) || (annex && strings.HasPrefix(citation, "A.")))
			}
			if !heading {
				if started && (!annex || annexStarted) && (strings.TrimSpace(line) != "" || (i > contentFirst && i < contentLast)) {
					if len(nodes) == 1 && strings.TrimSpace(line) != "" {
						return nil, fmt.Errorf("missing required region")
					}
					body.add(line)
					if strings.TrimSpace(line) != "" {
						passageNonempty = true
					}
				}
				continue
			}
			citation := match[1]
			if annex && !annexStarted && citation != "A.1" {
				continue
			}
			if !started {
				if pageIndex != 14 || i != 24 || citation != "5" {
					return nil, fmt.Errorf("missing required region")
				}
				started = true
			} else if len(nodes) == 1 && citation != "5.1" {
				return nil, fmt.Errorf("missing required region")
			}
			if annex && !annexStarted {
				if pageIndex != 66 || i != 7 {
					return nil, fmt.Errorf("missing required region")
				}
				annexStarted = true
			}
			if pending != "" {
				if citation != pending {
					return nil, fmt.Errorf("invalid or duplicate citations")
				}
				if !passageNonempty {
					return nil, fmt.Errorf("missing duplicate passage")
				}
				pending = ""
			}
			if _, exists := seen[citation]; exists {
				if (citation != "8.1.3.2" && citation != "9.1.3.2") || duplicate[citation] || previous != citation || before != citation[:1]+".1.3.1" {
					return nil, fmt.Errorf("invalid or duplicate citations")
				}
				if !passageNonempty {
					return nil, fmt.Errorf("missing duplicate passage")
				}
				duplicate[citation] = true
				passageNonempty = false
				body.gap = true
				body.add(line)
				pending = citation[:1] + ".1.4"
				before, previous = previous, citation
				continue
			}
			parent := -1
			if dot := strings.LastIndexByte(citation, '.'); dot >= 0 && !strings.HasPrefix(citation, "A.") || strings.Count(citation, ".") > 1 {
				parentCitation := citation[:strings.LastIndexByte(citation, '.')]
				var ok bool
				parent, ok = seen[parentCitation]
				if !ok {
					return nil, fmt.Errorf("missing heading parent")
				}
				active := len(nodes) - 1
				for active >= 0 && active != parent {
					active = nodes[active].Parent
				}
				if active < 0 {
					return nil, fmt.Errorf("missing heading parent")
				}
				nodes[parent].Kind = "clause"
			}
			closeBody()
			passageNonempty = false
			seen[citation] = len(nodes)
			nodes = append(nodes, Node{Citation: citation, Kind: "control", Parent: parent, Ordinal: len(nodes), PublisherTitle: match[2], Status: "active"})
			before, previous = previous, citation
		}
	}
	if !started || !annex || !annexStarted {
		return nil, fmt.Errorf("missing required region")
	}
	if pending != "" || !duplicate["8.1.3.2"] || !duplicate["9.1.3.2"] {
		return nil, fmt.Errorf("invalid or duplicate citations")
	}
	closeBody()
	for _, node := range nodes {
		if node.Kind == "control" && strings.TrimSpace(node.Body) == "" {
			return nil, fmt.Errorf("missing control body")
		}
	}
	return nodes, nil
}
