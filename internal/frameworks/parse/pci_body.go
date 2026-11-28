package parse

import (
	"fmt"
	"sort"
	"strings"
)

type pciBodyLine struct {
	page, block int
	key         pciKey
	geometry    pciBox
	text        string
	section     bool
}
type pciBody struct{ lines []pciBodyLine }

func (b *pciBody) add(line pciLine, page int) {
	if len(line.retained) == 0 {
		return
	}
	var parts []string
	var geometry pciBox
	for _, w := range line.retained {
		if !w.emit {
			continue
		}
		if len(parts) == 0 {
			geometry = w.box
		} else {
			if w.box.xmin < geometry.xmin {
				geometry.xmin = w.box.xmin
			}
			if w.box.ymin < geometry.ymin {
				geometry.ymin = w.box.ymin
			}
			if w.box.xmax > geometry.xmax {
				geometry.xmax = w.box.xmax
			}
			if w.box.ymax > geometry.ymax {
				geometry.ymax = w.box.ymax
			}
		}
		parts = append(parts, w.text)
	}
	text := strings.TrimSpace(strings.Join(parts, " "))
	if text == "" {
		return
	}
	b.lines = append(b.lines, pciBodyLine{page: page, block: line.block, key: pciOrderKey(line, -1), geometry: geometry, text: text, section: line.section})
}

func pciGlyph(s string) bool {
	switch s {
	case "•", "▪", "◦", "●", "○", "–", "-":
		return true
	}
	return false
}
func pciList(s string) bool {
	for _, g := range []string{"• ", "▪ ", "◦ ", "● ", "○ ", "– ", "- "} {
		if strings.HasPrefix(s, g) {
			return true
		}
	}
	return false
}
func (b *pciBody) text() (string, error) {
	sort.SliceStable(b.lines, func(i, j int) bool {
		a, c := b.lines[i], b.lines[j]
		if a.page != c.page {
			return a.page < c.page
		}
		return pciKeyLess(a.key, c.key)
	})
	joined, err := pciJoinGlyphs(b.lines)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	var previous pciBodyLine
	have := false
	for _, line := range joined {
		if have {
			switch {
			case pciList(line.text) || line.section:
				out.WriteByte('\n')
			case line.page == previous.page && line.block != previous.block:
				out.WriteString("\n\n")
			default:
				out.WriteByte(' ')
			}
		}
		out.WriteString(line.text)
		previous = line
		have = true
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("empty requirement body")
	}
	return out.String(), nil
}
