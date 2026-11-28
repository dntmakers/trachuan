package parse

import "strings"

type isoReader struct {
	pages                      []string
	page, line                 int
	rest                       string
	active, first, endFiltered bool
	pending                    []isoLine
	header, footer             string
	footerLast                 func(isoLine) bool
	headerSuffix               bool
	blankBefore, contentFirst  bool
}

func (r *isoReader) next() (isoLine, bool) {
	for {
		if !r.active {
			if r.page == len(r.pages) {
				return isoLine{}, false
			}
			r.rest = r.pages[r.page]
			r.page++
			r.line = 0
			r.active = true
			r.first = true
			r.endFiltered = false
			r.blankBefore = true
			r.contentFirst = true
		}
		if r.rest == "" {
			if !r.endFiltered {
				r.endFiltered = true
				hasFooter := false
				for _, line := range r.pending {
					if r.footer != "" && strings.Contains(line.text, r.footer) {
						hasFooter = true
					}
				}
				if r.footerLast != nil {
					if n := len(r.pending); n > 0 && r.footerLast(r.pending[n-1]) {
						r.pending = r.pending[:n-1]
					}
				} else if hasFooter {
					kept := r.pending[:0]
					for _, line := range r.pending {
						if !strings.Contains(line.text, r.footer) && !isoPageNumber(line.text) {
							kept = append(kept, line)
						}
					}
					r.pending = kept
				}
			}
			if len(r.pending) > 0 {
				line := r.pending[0]
				r.pending = r.pending[1:]
				return line, true
			}
			r.active = false
			continue
		}
		raw, rest, _ := strings.Cut(r.rest, "\n")
		r.line++
		r.rest = rest
		raw = strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(strings.NewReplacer("\uFEFF", "", "\b", "", "\u200B", "").Replace(raw))
		if text == "" {
			r.blankBefore = true
			continue
		}
		if r.first {
			r.first = false
			if text == r.header || (r.headerSuffix && strings.HasSuffix(text, r.header)) {
				continue
			}
		}
		line := isoLine{raw: raw, text: text, page: r.page, line: r.line, blankBefore: r.blankBefore, firstContent: r.contentFirst}
		r.blankBefore = false
		r.contentFirst = false
		r.pending = append(r.pending, line)
		if len(r.pending) <= 3 {
			continue
		}
		previous := r.pending[0]
		r.pending = r.pending[1:]
		return previous, true
	}
}

func isoPageNumber(text string) bool {
	for _, c := range text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return text != ""
}
