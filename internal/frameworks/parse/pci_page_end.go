package parse

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func pciPageEnd(lines []pciLine, h pciHeader, page int, table bool) error {
	if !table {
		return pciProjectTable(lines, h)
	}
	expected := false
	switch page {
	case 87, 89, 91, 95, 97, 101, 103, 204, 262, 270, 275, 285, 288, 290, 292, 327, 356, 367:
		expected = true
	}
	caption := -1
	for i := range lines {
		line := &lines[i]
		count := 0
		outside := false
		start, end, pageWord, closeWord := false, false, false, false
		var words [5]pciProjectedWord
		for _, w := range line.words {
			if w.box.xmin < h.left-5 || w.box.xmax > h.test-3 || strings.TrimSpace(w.text) == "" {
				continue
			}
			if w.box.ymin <= h.bottom || w.box.ymin >= h.footer {
				outside = true
			}
			switch w.text {
			case "(continued":
				start = true
			case "page)":
				end = true
			case "page":
				pageWord = true
			case ")":
				closeWord = true
			}
			if count < len(words) {
				words[count] = w
			}
			count++
		}
		fourShape := count == 4 && start && end
		splitShape := count == 5 && start && pageWord && closeWord
		if !fourShape && !splitShape {
			continue
		}
		for j := 0; j < count; j++ {
			for k := j + 1; k < count; k++ {
				if words[j].box.xmin == words[k].box.xmin {
					return fmt.Errorf("ambiguous page-end caption")
				}
			}
		}
		sort.Slice(words[:count], func(a, b int) bool { return words[a].box.xmin < words[b].box.xmin })
		if outside || words[0].text != "(continued" {
			return fmt.Errorf("invalid page-end caption")
		}
		if fourShape && words[count-1].text != "page)" {
			return fmt.Errorf("invalid page-end caption")
		}
		if splitShape {
			last, previous := words[count-1], words[count-2]
			gap := last.box.xmin - previous.box.xmax
			if last.text != ")" || previous.text != "page" || page != 275 || gap < 0 || math.Round(gap*100) != 4 {
				return fmt.Errorf("invalid page-end caption")
			}
		}
		if line.role != pciOther {
			return fmt.Errorf("page-end caption overlaps opener or heading")
		}
		if !expected || caption >= 0 {
			return fmt.Errorf("invalid page-end caption")
		}
		caption = i
		line.role = pciPageEndLabel
	}
	if expected && caption < 0 {
		return fmt.Errorf("missing page-end caption")
	}
	if err := pciProjectTable(lines, h); err != nil {
		return err
	}
	if caption < 0 {
		return nil
	}
	target := pciOrderKey(lines[caption], -1)
	for i, line := range lines {
		if i == caption {
			continue
		}
		hasLeft := false
		for _, w := range line.retained {
			if w.box.xmin >= h.left-5 && w.box.xmax <= h.test-3 && w.box.ymin > h.bottom && w.box.ymin < h.footer {
				hasLeft = true
				break
			}
		}
		if hasLeft && !pciKeyLess(pciOrderKey(line, -1), target) {
			return fmt.Errorf("invalid page-end caption")
		}
	}
	return nil
}
