package parse

import (
	"fmt"
	"math"
	"strconv"

	"danny.vn/trachuan/internal/pdftext"
)

type pciBox struct{ xmin, ymin, xmax, ymax float64 }

func pciBoxOf(xmin, ymin, xmax, ymax string) (pciBox, error) {
	var b pciBox
	values := []*float64{&b.xmin, &b.ymin, &b.xmax, &b.ymax}
	for i, s := range []string{xmin, ymin, xmax, ymax} {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return b, fmt.Errorf("invalid coordinates")
		}
		*values[i] = v
	}
	if b.xmin > b.xmax || b.ymin > b.ymax {
		return b, fmt.Errorf("invalid coordinates")
	}
	return b, nil
}
func pciWordBox(w pdftext.Word) (pciBox, error) { return pciBoxOf(w.XMin, w.YMin, w.XMax, w.YMax) }
func pciRound(x float64) float64                { return math.Round(x*10) / 10 }
