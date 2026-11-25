package retrieval

import "unicode"

type snippetRange struct{ start, end int }

type sourcePosition struct {
	char     rune
	retained bool
	body     bool
	overlaps bool
}

func SnippetWithBodyCut(h Hit, capRunes int) (string, bool, bool) {
	text, complete, ranges := snippetRanges(h, capRunes)
	source := []rune(h.Text)
	positions := map[int]sourcePosition{}
	overlaps := make([]bool, len(source))
	for _, f := range h.Fragments {
		if f.Identity == h.Identity || f.Role != "body" {
			continue
		}
		if f.TextStart < 0 || f.TextEnd < f.TextStart || f.TextEnd > len(source) {
			return text, complete, false
		}
		for i := f.TextStart; i < f.TextEnd; i++ {
			overlaps[i] = true
		}
	}
	for _, f := range h.Fragments {
		if f.Identity != h.Identity {
			continue
		}
		if f.Start < 0 || f.End < f.Start || f.TextStart < 0 || f.TextEnd < f.TextStart || f.TextEnd > len(source) || f.End-f.Start != f.TextEnd-f.TextStart {
			return text, complete, false
		}
		for i := f.TextStart; i < f.TextEnd; i++ {
			p := f.Start + i - f.TextStart
			ch := source[i]
			old, exists := positions[p]
			if exists && old.char != ch {
				return text, complete, false
			}
			kept := old.retained
			for _, r := range ranges {
				if i >= r.start && i < r.end {
					kept = true
					break
				}
			}
			positions[p] = sourcePosition{char: ch, retained: kept, body: old.body || f.Role == "body", overlaps: old.overlaps || overlaps[i]}
		}
	}
	kept, omitted := false, false
	for _, p := range positions {
		if p.body && !p.overlaps && !unicode.IsSpace(p.char) {
			if p.retained {
				kept = true
			} else {
				omitted = true
			}
		}
	}
	return text, complete, kept && omitted
}
