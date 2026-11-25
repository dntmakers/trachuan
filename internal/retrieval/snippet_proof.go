package retrieval

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type snippetRange struct{ start, end int }

type sourceKey struct {
	identity Identity
	offset   int
}

type sourcePosition struct {
	char    rune
	body    bool
	heading bool
}

func normalizedLabel(s string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(s)))
}

func ruleLeaves(nodes []Provision) map[Identity]bool {
	parents := map[int64]bool{}
	for _, p := range nodes {
		if p.ParentID != nil {
			parents[*p.ParentID] = true
		}
	}
	leaves := map[Identity]bool{}
	for _, p := range nodes {
		if !parents[p.ID] {
			leaves[p.Identity] = true
		}
	}
	return leaves
}

func matchPrefix(source []rune, value string) (int, bool) {
	want := normalizedLabel(value)
	if want == "" {
		return 0, true
	}
	limit := min(len(source), len([]rune(value))*3+8)
	for i := 1; i <= limit; i++ {
		if normalizedLabel(string(source[:i])) == want {
			return i, true
		}
	}
	return 0, false
}

func printedLabel(source []rune, label string) (int, bool) {
	if label == "" {
		return 0, true
	}
	if n, ok := matchPrefix(source, label); ok {
		end := n
		if end < len(source) && (source[end] == '.' || source[end] == ':') {
			end++
		}
		if end == len(source) || unicode.IsSpace(source[end]) {
			return end, true
		}
	}
	fields := strings.Fields(label)
	if len(fields) < 2 {
		return 0, false
	}
	n, ok := matchPrefix(source, fields[len(fields)-1])
	if !ok || n >= len(source) || (source[n] != '.' && source[n] != ')') {
		return 0, false
	}
	if n+1 < len(source) && !unicode.IsSpace(source[n+1]) {
		return 0, false
	}
	return n + 1, true
}

func rulePrefixes(nodes []Provision) map[Identity]int {
	out := make(map[Identity]int, len(nodes))
	for _, p := range nodes {
		runes := []rune(p.Text)
		start := 0
		for start < len(runes) && unicode.IsSpace(runes[start]) {
			start++
		}
		end, ok := printedLabel(runes[start:], strings.TrimSpace(p.Label))
		if !ok {
			continue
		}
		end += start
		heading := strings.TrimSpace(p.Heading)
		if heading == "" {
			out[p.Identity] = end
			continue
		}
		lineEnd := end
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		if normalizedLabel(strings.TrimSpace(string(runes[end:lineEnd]))) != normalizedLabel(heading) {
			continue
		}
		out[p.Identity] = lineEnd
	}
	return out
}

func SnippetWithBodyCut(h Hit, capRunes int) (string, bool, bool) {
	text, complete, ranges := snippetRanges(h, capRunes)
	if h.ambiguousTree {
		return text, complete, false
	}
	source := []rune(h.Text)
	bodyIDs := map[Identity]bool{}
	for _, f := range h.Fragments {
		if f.Role == "body" {
			bodyIDs[f.Identity] = true
		}
	}
	positions := map[sourceKey]sourcePosition{}
	parent := map[sourceKey]sourceKey{}
	at := make(map[int]sourceKey)
	var root func(sourceKey) sourceKey
	root = func(k sourceKey) sourceKey {
		if parent[k] != k {
			parent[k] = root(parent[k])
		}
		return parent[k]
	}
	for _, f := range h.Fragments {
		if f.Start < 0 || f.End < f.Start || f.TextStart < 0 || f.TextEnd < f.TextStart || f.TextEnd > len(source) || f.End-f.Start != f.TextEnd-f.TextStart {
			return text, complete, false
		}
		for i := f.TextStart; i < f.TextEnd; i++ {
			k := sourceKey{f.Identity, f.Start + i - f.TextStart}
			p, exists := positions[k]
			if exists && p.char != source[i] {
				return text, complete, false
			}
			if !exists {
				parent[k] = k
			}
			prefix, known := h.rulePrefixes[f.Identity]
			positions[k] = sourcePosition{char: source[i], body: p.body || (f.Role == "body" && known && h.ruleLeaves[f.Identity] && k.offset >= prefix), heading: p.heading || (bodyIDs[f.Identity] && !known) || (f.Role == "body" && known && k.offset < prefix)}
			if other, ok := at[i]; ok {
				parent[root(k)] = root(other)
			} else {
				at[i] = k
			}
		}
	}
	retained := map[sourceKey]bool{}
	for i, k := range at {
		for _, r := range ranges {
			if i >= r.start && i < r.end {
				retained[root(k)] = true
				break
			}
		}
	}
	headings := map[sourceKey]bool{}
	for k, p := range positions {
		if p.heading {
			headings[root(k)] = true
		}
	}
	kept := map[sourceKey]bool{}
	omitted := map[sourceKey]bool{}
	for k, p := range positions {
		if !p.body || headings[root(k)] || (!unicode.IsLetter(p.char) && !unicode.IsDigit(p.char)) {
			continue
		}
		if retained[root(k)] {
			kept[root(k)] = true
		} else {
			omitted[root(k)] = true
		}
	}
	return text, complete, len(kept) >= minimumSampleRuleChars && len(omitted) >= minimumSampleRuleChars
}
