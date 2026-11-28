package parse

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
)

type pciProjectedWord struct {
	text string
	box  pciBox
	emit bool
}
type pciLine struct {
	box                   pciBox
	lineTop, retainedTop  float64
	minX, maxX            float64
	firstText, secondText string
	citation              string
	second                pciBox
	words                 []pciProjectedWord
	retained              []pciProjectedWord
	role                  pciLabel
	block                 int
	first                 bool
	blockWords            int
	owner                 int
	section               bool
}

func pciHasContent(line pciLine) bool {
	for _, w := range line.retained {
		if w.emit && strings.TrimSpace(w.text) != "" {
			return true
		}
	}
	return false
}
func pciIsSection(words []pciProjectedWord) bool {
	var first [3]pciProjectedWord
	n := 0
	for _, word := range words {
		if !word.emit {
			continue
		}
		first[n] = word
		n++
		if n == len(first) {
			break
		}
	}
	if n == 0 {
		return false
	}
	box := first[0].box
	if box.xmin < 54 || box.xmin > 295 || box.ymin < 79 || box.ymin >= 530 {
		return false
	}
	if n >= 3 && first[0].text == "Customized" && first[2].text == "Objective" {
		return true
	}
	return n >= 2 && first[0].text == "Applicability" && first[1].text == "Notes"
}

type pciKey struct{ y, x float64 }

func pciOrderKey(line pciLine, wordIndex int) pciKey {
	if len(line.retained) == 0 {
		return pciKey{math.Inf(1), math.Inf(1)}
	}
	key := pciKey{y: math.Max(line.lineTop, line.retainedTop), x: math.Inf(1)}
	if wordIndex >= 0 {
		key.x = line.retained[wordIndex].box.xmin
		return key
	}
	anchor := ""
	switch line.role {
	case pciRoot:
		anchor = line.secondText
	case pciLineGroup, pciLineRow, pciLineCaption:
		anchor = line.citation
	case pciLineStandalone:
		anchor = line.firstText
	}
	if anchor != "" {
		for _, w := range line.retained {
			if !w.emit && w.text == anchor {
				key.x = w.box.xmin
				return key
			}
		}
	}
	for _, w := range line.retained {
		if w.box.xmin < key.x {
			key.x = w.box.xmin
		}
	}
	return key
}
func pciKeyLess(a, b pciKey) bool { return a.y < b.y || a.y == b.y && a.x < b.x }
func pciOrderProjectedLines(lines []pciLine) {
	sort.SliceStable(lines, func(i, j int) bool { return pciKeyLess(pciOrderKey(lines[i], -1), pciOrderKey(lines[j], -1)) })
}

func pciClassifyPage(page pdftext.Page) ([]pciLine, error) {
	var lines []pciLine
	for bi, block := range page.Blocks {
		count, start := 0, len(lines)
		for li, line := range block.Lines {
			count += len(line.Words)
			if len(line.Words) == 0 {
				continue
			}
			top, topErr := strconv.ParseFloat(line.YMin, 64)
			if topErr != nil || math.IsNaN(top) || math.IsInf(top, 0) {
				return nil, fmt.Errorf("invalid line coordinates")
			}
			item := pciLine{block: bi, first: li == 0, owner: -1, role: pciClassifyLine(line, li == 0), lineTop: top}
			for wi, w := range line.Words {
				box, err := pciWordBox(w)
				if err != nil {
					return nil, err
				}
				if wi == 0 {
					item.box = box
					item.firstText = w.Text
					item.minX = box.xmin
					item.maxX = box.xmax
				}
				if box.xmin < item.minX {
					item.minX = box.xmin
				}
				if box.xmax > item.maxX {
					item.maxX = box.xmax
				}
				if wi == 1 {
					item.second = box
					item.secondText = w.Text
				}
				if box.ymax > item.box.ymax {
					item.box.ymax = box.ymax
				}
				item.words = append(item.words, pciProjectedWord{text: w.Text, box: box, emit: true})
			}
			lines = append(lines, item)
		}
		for i := start; i < len(lines); i++ {
			lines[i].blockWords = count
		}
	}
	return lines, nil
}
