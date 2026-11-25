package retrieval

import (
	"strings"
	"unicode"
)

// OmissionMarker marks source text omitted from a snippet.
const OmissionMarker = "[…]"

// Snippet returns a bounded verbatim excerpt and whether it is complete.
func Snippet(h Hit, capRunes int) (text string, complete bool) {
	text, complete, _ = snippetRanges(h, capRunes)
	return text, complete
}

func snippetRanges(h Hit, capRunes int) (text string, complete bool, ranges []snippetRange) {
	source := []rune(h.Text)
	if capRunes < 0 {
		capRunes = 0
	}
	if len(source) <= capRunes {
		return h.Text, h.Complete, []snippetRange{{0, len(source)}}
	}
	if capRunes < len([]rune(OmissionMarker)) {
		return "", false, nil
	}

	leadEnd := 0
	for _, f := range h.Fragments {
		if f.Role == "lead" && f.TextEnd > leadEnd {
			leadEnd = min(f.TextEnd, len(source))
		}
	}
	ownStart := leadEnd
	if leadEnd > 0 && ownStart < len(source) && source[ownStart] == '\n' {
		ownStart++
	}
	best := max(ownStart, min(h.BestTextStart, len(source)))

	// Reserve room for a trailing marker, and for an internal omission if needed.
	markerLen := len([]rune(OmissionMarker))
	leadLimit := capRunes / 2
	var lead []rune
	leadCut := false
	if leadEnd > 0 {
		if leadEnd <= leadLimit {
			lead = source[:leadEnd]
		} else {
			available := leadLimit - markerLen - 1
			if available > 0 {
				lead = source[:cutAt(source[:leadEnd], available)]
			}
			leadCut = true
		}
	}
	out := string(lead)
	if leadCut {
		out = strings.TrimRightFunc(out, unicode.IsSpace)
	}
	if len([]rune(out)) > 0 {
		ranges = append(ranges, snippetRange{0, len([]rune(out))})
	}
	omitted := leadCut || best > ownStart
	if out != "" && len([]rune(out))+markerLen+1 > capRunes {
		return OmissionMarker, false, nil
	}
	if omitted && out != "" {
		out += " " + OmissionMarker
	}

	separator := ""
	if out != "" {
		if omitted {
			separator = " "
		} else {
			separator = "\n"
		}
	} else if omitted {
		out = OmissionMarker
		separator = " "
	}
	remaining := capRunes - len([]rune(out)) - len([]rune(separator))
	body := source[best:]
	if len(body) <= remaining {
		out += separator + string(body)
		ranges = append(ranges, snippetRange{best, len(source)})
		return out, false, ranges
	}
	remaining -= markerLen + 1
	if remaining > 0 {
		end := cutAt(body, remaining)
		retained := strings.TrimRightFunc(string(body[:end]), unicode.IsSpace)
		out += separator + retained
		ranges = append(ranges, snippetRange{best, best + len([]rune(retained))})
	}
	if out == "" {
		return OmissionMarker, false, nil
	}
	if !strings.HasSuffix(out, OmissionMarker) && len([]rune(out))+1+markerLen <= capRunes {
		out += " " + OmissionMarker
	}
	return out, false, ranges
}

// cutAt selects a word or combining-mark-safe boundary within limit.
func cutAt(r []rune, limit int) int {
	if len(r) == 0 {
		return 0
	}
	limit = max(1, limit)
	if limit >= len(r) {
		return len(r)
	}
	if unicode.IsSpace(r[limit]) {
		return limit
	}
	for i := limit; i > 0; i-- {
		if unicode.IsSpace(r[i-1]) {
			return i
		}
	}
	end := limit
	for end > 0 && unicode.Is(unicode.Mn, r[end]) {
		end--
	}
	if end == 0 {
		return limit
	}
	return end
}
