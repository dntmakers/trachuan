package parse

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func pciStructural(role pciLabel) bool {
	return role == pciPageEndLabel || role == pciOuterRequirements || role == pciOuterGuidance || role == pciSubRequirements || role == pciSubTesting || role == pciFooterLabel || role == pciCopyright || role == pciDate || role == pciPageNumber || role == pciRoot || role == pciMalformed
}
func pciClassifyTable(lines []pciLine, h pciHeader, table bool, page int) error {
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
	}
	return pciPageEnd(lines, h, page, table)
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
		if pciStructural(line.role) || line.role == pciLineIgnore {
			continue
		}
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
