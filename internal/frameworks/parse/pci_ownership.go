package parse

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func pciStructural(role pciLabel) bool {
	return role == pciOuterRequirements || role == pciOuterGuidance || role == pciSubRequirements || role == pciSubTesting || role == pciFooterLabel || role == pciCopyright || role == pciDate || role == pciPageNumber || role == pciRoot || role == pciMalformed
}
func pciClassifyTable(lines []pciLine, h pciHeader, table bool) error {
	groups, roots := map[int]bool{}, map[int]bool{}
	for _, line := range lines {
		if line.role == pciRoot {
			roots[line.block] = true
		}
	}
	for i := range lines {
		line := &lines[i]
		if line.role != pciOther || !table || line.box.ymin <= h.bottom || line.box.ymin >= h.footer {
			continue
		}
		b := line.box
		word := line.firstText
		if line.blockWords == 1 && word == "(continued)" {
			if b.xmax < h.left-5 || b.xmin > h.test-3 {
				continue
			}
			if b.xmin < h.left-5 || b.xmax > h.test-3 {
				return fmt.Errorf("invalid standalone caption")
			}
			line.role = pciLineStandalone
			continue
		}
		if !line.first || b.xmin < h.left-1.5 || b.xmin > h.left+6 {
			continue
		}
		if !pciID.MatchString(word) {
			if pciIDCandidate.MatchString(word) {
				return fmt.Errorf("invalid requirement")
			}
			continue
		}
		line.citation = word
		marker := line.second
		if line.secondText == "(continued)" && b.xmin >= h.left-5 && b.xmax <= h.test-3 && marker.xmin >= h.left-5 && marker.xmax <= h.test-3 && marker.ymin > h.bottom && marker.ymin < h.footer {
			line.role = pciLineCaption
			continue
		}
		if h.paired && b.ymin < h.firstPair {
			if strings.Count(word, ".") != 1 {
				return fmt.Errorf("invalid spanning group")
			}
			line.role = pciLineGroup
			groups[line.block] = true
		} else {
			line.role = pciLineRow
		}
	}
	for i := range lines {
		line := &lines[i]
		if line.role == pciOther {
			switch {
			case roots[line.block]:
				line.role = pciLineHeadingPart
			case groups[line.block]:
				line.role = pciLineGroupPart
			case !table:
				line.role = pciLineIgnore
			}
		}
		if pciStructural(line.role) && line.role != pciRoot {
			line.words = nil
			continue
		}
		if line.role == pciLineIgnore {
			line.words = nil
			continue
		}
		full := line.role == pciRoot || line.role == pciLineHeadingPart || line.role == pciLineGroup || line.role == pciLineGroupPart
		for wi, w := range line.words {
			if line.role != pciRoot && line.role != pciLineHeadingPart && (w.box.ymin <= h.bottom || w.box.ymin >= h.footer) {
				continue
			}
			if !full && (w.box.xmin < h.test-3 && w.box.xmax > h.test-3 || w.box.xmin < h.right && w.box.xmax > h.right) {
				return fmt.Errorf("crossing table band")
			}
			if (line.role == pciRoot && wi < 2) || (line.role == pciLineGroup && wi < 1) || (line.role == pciLineRow && wi < 1) || (line.role == pciLineCaption && wi < 2) || (line.role == pciLineStandalone && wi == 0) {
				w.emit = false
			}
			if w.emit && strings.TrimSpace(w.text) == "" {
				continue
			}
			if !full && (w.box.xmin < h.left-5 || w.box.xmax > h.test-3) {
				continue
			}
			if len(line.retained) == 0 || w.box.ymin < line.retainedTop {
				line.retainedTop = w.box.ymin
			}
			line.retained = append(line.retained, w)
		}
		sort.SliceStable(line.retained, func(i, j int) bool {
			return line.retained[i].box.xmin < line.retained[j].box.xmin
		})
		line.section = pciIsSection(line.retained)
		if line.role == pciOther {
			if pciHasContent(*line) {
				line.role = pciLineBody
			} else {
				line.role = pciLineIgnore
			}
		}
		line.words = nil
	}
	for _, line := range lines {
		if line.role == pciOther {
			return fmt.Errorf("undecided line role")
		}
	}
	return nil
}
func pciRejectCrossingOpeners(lines []pciLine, h pciHeader) error {
	type opener struct {
		key   pciKey
		block int
	}
	var openers []opener
	for _, line := range lines {
		if line.role == pciLineRow || line.role == pciLineGroup {
			if len(line.retained) == 0 {
				return fmt.Errorf("missing opener anchor")
			}
			key := pciOrderKey(line, -1)
			if len(openers) > 0 && key.y-openers[len(openers)-1].key.y <= 0.5 {
				return fmt.Errorf("ambiguous line ownership")
			}
			openers = append(openers, opener{key: key, block: line.block})
		}
	}
	starts := map[int]pciKey{}
	for _, line := range lines {
		if line.role == pciLineIgnore || pciStructural(line.role) {
			continue
		}
		if len(line.retained) == 0 && line.role != pciLineRow && line.role != pciLineGroup && line.role != pciLineCaption {
			continue
		}
		if _, ok := starts[line.block]; !ok {
			starts[line.block] = pciOrderKey(line, -1)
		}
	}
	for _, line := range lines {
		key := pciOrderKey(line, -1)
		if line.role == pciLineBody && len(line.retained) > 0 {
			at := sort.Search(len(openers), func(i int) bool { return openers[i].key.y >= key.y-0.5 })
			for at < len(openers) && openers[at].key.y <= key.y+0.5 {
				if openers[at].block != line.block && math.Abs(openers[at].key.y-key.y) <= 0.5 {
					return fmt.Errorf("ambiguous line ownership")
				}
				at++
			}
		}
		start, ok := starts[line.block]
		if !ok || !pciKeyLess(start, key) {
			continue
		}
		next := sort.Search(len(openers), func(i int) bool { return pciKeyLess(start, openers[i].key) })
		if next == len(openers) || pciKeyLess(key, openers[next].key) {
			continue
		}
		for _, w := range line.retained {
			if w.box.xmin >= h.left-5 && w.box.xmax <= h.test-3 {
				return fmt.Errorf("ambiguous line ownership")
			}
		}
	}
	return nil
}
