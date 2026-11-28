package parse

import "strings"

type isoReader struct {
	pages                      []string
	page, line                 int
	rest                       string
	active, first, endFiltered bool
	pending                    []isoLine
	header, footer             string
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
				if hasFooter {
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
		text := strings.TrimSpace(strings.NewReplacer("\uFEFF", "", "\b", "").Replace(raw))
		if text == "" {
			continue
		}
		if r.first {
			r.first = false
			if text == r.header {
				continue
			}
		}
		line := isoLine{raw: raw, text: text, page: r.page, line: r.line}
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
