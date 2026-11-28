package parse

import (
	"fmt"
	"sort"
	"strings"
)

func pciProjectTable(lines []pciLine, h pciHeader) error {
	for i := range lines {
		line := &lines[i]
		if pciStructural(line.role) && line.role != pciRoot && line.role != pciSubRequirements && line.role != pciPageEndLabel {
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
			if (line.role == pciRoot && wi < 2) || (line.role == pciLineGroup && wi < 1) || (line.role == pciLineRow && wi < 1) || (line.role == pciLineCaption && wi < 2) || (line.role == pciLineStandalone && wi == 0) || line.role == pciPageEndLabel || line.role == pciSubRequirements {
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
		sort.SliceStable(line.retained, func(i, j int) bool { return line.retained[i].box.xmin < line.retained[j].box.xmin })
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
	return nil
}
