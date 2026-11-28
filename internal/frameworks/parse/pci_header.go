package parse

import (
	"fmt"
	"math"
)

type pciHeader struct {
	outerLeft, outerRight, bottom, footer, left, test, right, firstPair float64
	paired                                                              bool
}
type pciHeaderWord struct{ box pciBox }

func pciHeaderForLines(lines []pciLine, previous map[[2]float64]pciHeader) (pciHeader, bool, error) {
	var h pciHeader
	var outerL, outerR, left, right []pciHeaderWord
	var footers, copyrights, dates, pageNumbers []pciBox
	malformed := false
	lastLeft := 0.0
	for _, line := range lines {
		b := line.box
		if b.ymin >= 530 && b.xmin < 600 && b.ymin > lastLeft {
			lastLeft = b.ymin
		}
		switch line.role {
		case pciMalformed:
			malformed = true
		case pciFooterLabel:
			footers = append(footers, b)
		case pciCopyright:
			copyrights = append(copyrights, b)
		case pciDate:
			dates = append(dates, b)
		case pciPageNumber:
			pageNumbers = append(pageNumbers, b)
		case pciOuterRequirements:
			b.xmax = line.maxX
			outerL = append(outerL, pciHeaderWord{b})
		case pciOuterGuidance:
			outerR = append(outerR, pciHeaderWord{b})
		case pciSubRequirements:
			left = append(left, pciHeaderWord{b})
		case pciSubTesting:
			right = append(right, pciHeaderWord{b})
		}
	}
	if malformed {
		return h, false, fmt.Errorf("invalid structural label")
	}
	if len(outerL) == 0 && len(outerR) == 0 {
		return h, false, nil
	}
	if len(footers) != 1 || len(copyrights) != 1 || len(dates) != 1 || len(pageNumbers) != 1 || lastLeft > copyrights[0].ymin+0.5 || math.Abs(dates[0].ymin-footers[0].ymin) > 0.5 || math.Abs(pageNumbers[0].ymin-copyrights[0].ymin) > 0.5 || copyrights[0].ymin <= footers[0].ymin || copyrights[0].ymin-footers[0].ymin > 20 {
		return h, false, fmt.Errorf("invalid table footer")
	}
	for _, line := range lines {
		b := line.box
		if b.xmin < 600 && b.ymin > footers[0].ymin+0.5 && b.ymin < copyrights[0].ymin-0.5 {
			return h, false, fmt.Errorf("invalid table footer")
		}
	}
	h.footer = footers[0].ymin
	if len(outerL) != 1 || len(outerR) != 1 || math.Abs(outerL[0].box.ymin-outerR[0].box.ymin) > 0.5 {
		return h, false, fmt.Errorf("invalid table furniture")
	}
	h.outerLeft = pciRound(outerL[0].box.xmin)
	h.outerRight = pciRound(outerR[0].box.xmin)
	h.bottom = math.Max(outerL[0].box.ymax, outerR[0].box.ymax)
	used := make(map[int]bool)
	for _, l := range left {
		if l.box.ymin <= h.bottom || l.box.ymin >= h.footer {
			return h, false, fmt.Errorf("invalid paired header")
		}
		match := -1
		for i, r := range right {
			if math.Abs(l.box.ymin-r.box.ymin) <= 0.5 {
				if match >= 0 || used[i] {
					return h, false, fmt.Errorf("invalid paired header")
				}
				match = i
			}
		}
		if match < 0 {
			return h, false, fmt.Errorf("invalid paired header")
		}
		used[match] = true
		r := right[match]
		if r.box.ymin <= h.bottom || r.box.ymin >= h.footer {
			return h, false, fmt.Errorf("invalid paired header")
		}
		lx, tx := pciRound(l.box.xmin), pciRound(r.box.xmin)
		if h.paired && (h.left != lx || h.test != tx) {
			return h, false, fmt.Errorf("inconsistent bands")
		}
		if !h.paired || l.box.ymin < h.firstPair {
			h.firstPair = l.box.ymin
		}
		h.left, h.test, h.paired = lx, tx, true
	}
	if len(left) != len(right) {
		return h, false, fmt.Errorf("invalid paired header")
	}
	if !h.paired {
		prior, ok := previous[[2]float64{h.outerLeft, h.outerRight}]
		if !ok {
			return h, false, fmt.Errorf("missing paired header")
		}
		h.left, h.test = prior.left, prior.test
	}
	h.right = outerL[0].box.xmin + outerL[0].box.xmax - h.left + 4
	for _, line := range lines {
		if line.role != pciSubRequirements && line.role != pciSubTesting {
			continue
		}
		if line.role == pciSubRequirements && (line.minX < h.left-5 || line.maxX > h.test-3) {
			return h, false, fmt.Errorf("invalid structural label")
		}
		if line.role == pciSubTesting && (line.minX < h.test-3 || line.maxX > h.right) {
			return h, false, fmt.Errorf("invalid structural label")
		}
	}
	return h, true, nil
}
