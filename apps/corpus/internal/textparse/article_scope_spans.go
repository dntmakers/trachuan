package textparse

import "unicode/utf8"

type ArticleScopeSpan struct {
	Start, End int
	Kind       string
}

func ArticleScopeSpans(paragraphs []Paragraph) []ArticleScopeSpan {
	characters := 0
	for _, paragraph := range paragraphs {
		characters += utf8.RuneCountInString(paragraph.Text)
	}
	if characters < 300 {
		return nil
	}
	quoted, _ := quotedParagraphs(paragraphs)
	spans := scopes(paragraphs, quoted)
	out := make([]ArticleScopeSpan, len(spans))
	for i, s := range spans {
		out[i] = ArticleScopeSpan{Start: s.start, End: s.end, Kind: s.kind}
	}
	return out
}

func HasMainBeforeExplicitTail(paragraphs []Paragraph) bool {
	quoted, _ := quotedParagraphs(paragraphs)
	for i, p := range paragraphs {
		if quoted[i] {
			continue
		}
		if appendix(p.Text) {
			return false
		}
		if _, ok := article(p.Text); ok {
			return true
		}
	}
	return false
}
