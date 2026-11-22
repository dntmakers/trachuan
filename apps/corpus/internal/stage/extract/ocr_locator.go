package extract

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"danny.vn/trachuan/apps/corpus/internal/textparse"
	"golang.org/x/text/unicode/norm"
)

const ocrReadingBudget int64 = 100_000_000
const maxOCRReadingBytes = 4096

type ocrLocation struct {
	TailScopeIndex, StartParagraph, EndParagraph, StartLine, EndLine int
}

type ocrReadingPosition struct {
	Position string
	Location *ocrLocation
}

type ocrLocator struct {
	text       string
	paragraphs []textparse.Paragraph
	starts     []int
	tails      []int
	prefix     []int
}

func locateOCRReadings(text string, readings []string, budget int64) []ocrReadingPosition {
	out := make([]ocrReadingPosition, len(readings))
	for i := range out {
		out[i].Position = "unlocated"
	}
	paragraphs, err := textparse.ParagraphsFromText(text)
	if err != nil {
		return out
	}
	return locateOCRParsed(paragraphs, textparse.ArticleScopeSpans(paragraphs), readings, budget)
}

func locateOCRParsed(paragraphs []textparse.Paragraph, spans []textparse.ArticleScopeSpan, readings []string, budget int64) []ocrReadingPosition {
	out := make([]ocrReadingPosition, len(readings))
	for i := range out {
		out[i].Position = "unlocated"
	}
	if len(spans) == 0 || spans[0].Kind != "main" || !textparse.HasMainBeforeExplicitTail(paragraphs) {
		return out
	}
	locator := ocrLocator{paragraphs: paragraphs, starts: make([]int, len(paragraphs)), tails: make([]int, len(paragraphs)), prefix: make([]int, len(paragraphs)+1)}
	for i := range locator.tails {
		locator.tails[i] = -1
	}
	for i, span := range spans {
		if span.Kind == "tail" {
			for p := span.Start; p < span.End; p++ {
				locator.tails[p] = i
			}
		}
	}
	var flat strings.Builder
	for i, paragraph := range paragraphs {
		if i > 0 {
			flat.WriteByte(' ')
		}
		locator.starts[i] = flat.Len()
		flat.WriteString(paragraph.Text)
		locator.prefix[i+1] = locator.prefix[i]
		if locator.tails[i] >= 0 {
			locator.prefix[i+1]++
		}
	}
	locator.text = flat.String()
	indices := make(map[string][]int, len(readings))
	for i, reading := range readings {
		normalized := normalizeOCRReading(reading)
		indices[normalized] = append(indices[normalized], i)
	}
	keys := make([]string, 0, len(indices))
	for key := range indices {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if budget > ocrReadingBudget {
		budget = ocrReadingBudget
	}
	for _, key := range keys {
		if len(key) == 0 || len(key) > maxOCRReadingBytes {
			continue
		}
		cost := int64(len(locator.text)) + int64(len(key))
		if cost > budget {
			continue
		}
		budget -= cost
		result := locator.classify(key)
		for _, i := range indices[key] {
			out[i] = result
		}
	}
	return out
}

func (l ocrLocator) classify(pattern string) ocrReadingPosition {
	prefix := make([]int, len(pattern))
	for i, j := 1, 0; i < len(pattern); i++ {
		for j > 0 && pattern[i] != pattern[j] {
			j = prefix[j-1]
		}
		if pattern[i] == pattern[j] {
			j++
		}
		prefix[i] = j
	}
	result := ocrReadingPosition{Position: "unlocated"}
	for i, j := 0, 0; i < len(l.text); i++ {
		for j > 0 && l.text[i] != pattern[j] {
			j = prefix[j-1]
		}
		if l.text[i] == pattern[j] {
			j++
		}
		if j != len(pattern) {
			continue
		}
		start, end := i+1-len(pattern), i+1
		j = prefix[j-1]
		if !ocrTokenBoundary(l.text, start) || !ocrTokenBoundary(l.text, end) {
			continue
		}
		first := sort.Search(len(l.starts), func(n int) bool { return l.starts[n] > start }) - 1
		last := sort.Search(len(l.starts), func(n int) bool { return l.starts[n] > end-1 }) - 1
		count := l.prefix[last+1] - l.prefix[first]
		position := "not_tail"
		if count > 0 {
			position = "ambiguous"
			if count == last-first+1 && l.tails[first] == l.tails[last] {
				position = "tail"
			}
		}
		if result.Position == "unlocated" {
			result.Position = position
			if position == "tail" {
				result.Location = &ocrLocation{TailScopeIndex: l.tails[first], StartParagraph: l.paragraphs[first].Ordinal, EndParagraph: l.paragraphs[last].Ordinal, StartLine: l.paragraphs[first].Locator, EndLine: l.paragraphs[last].Locator}
			}
		} else if result.Position != position || position == "ambiguous" {
			result = ocrReadingPosition{Position: "ambiguous"}
		}
	}
	return result
}

func normalizeOCRReading(reading string) string {
	return norm.NFC.String(strings.Join(strings.Fields(reading), " "))
}

func ocrCore(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || r == '_'
}

func ocrJoiner(r rune) bool {
	return r == '/' || r == '.' || r == '-' || (r >= '‐' && r <= '—') || r == '−'
}

func ocrTokenBoundary(text string, at int) bool {
	if at == 0 || at == len(text) {
		return true
	}
	left, leftSize := utf8.DecodeLastRuneInString(text[:at])
	right, rightSize := utf8.DecodeRuneInString(text[at:])
	if ocrCore(left) && ocrCore(right) {
		return false
	}
	if ocrCore(left) && ocrJoiner(right) && at+rightSize < len(text) {
		next, _ := utf8.DecodeRuneInString(text[at+rightSize:])
		return !ocrCore(next)
	}
	if ocrJoiner(left) && ocrCore(right) && at-leftSize > 0 {
		previous, _ := utf8.DecodeLastRuneInString(text[:at-leftSize])
		return !ocrCore(previous)
	}
	return true
}
