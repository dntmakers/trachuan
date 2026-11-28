package parse

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
)

type cobitBox struct {
	x, y, xmax float64
	text       string
}
type cobitRow struct {
	y     float64
	words []cobitBox
}

func cobitRows(page pdftext.Page) ([]cobitRow, error) {
	boxes := make([]cobitBox, 0, len(page.Words))
	for _, w := range page.Words {
		x, e1 := strconv.ParseFloat(w.XMin, 64)
		y, e2 := strconv.ParseFloat(w.YMin, 64)
		xmax, e3 := strconv.ParseFloat(w.XMax, 64)
		ymax, e4 := strconv.ParseFloat(w.YMax, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) || math.IsNaN(xmax) || math.IsInf(xmax, 0) || math.IsNaN(ymax) || math.IsInf(ymax, 0) || xmax < x || ymax < y {
			return nil, fmt.Errorf("invalid word coordinates")
		}
		boxes = append(boxes, cobitBox{x: x, y: y, xmax: xmax, text: w.Text})
	}
	sort.SliceStable(boxes, func(i, j int) bool { return boxes[i].y < boxes[j].y })
	var rows []cobitRow
	for _, b := range boxes {
		if len(rows) == 0 || b.y-rows[len(rows)-1].y > 1 {
			rows = append(rows, cobitRow{y: b.y})
		}
		i := len(rows) - 1
		rows[i].words = append(rows[i].words, b)
	}
	for i := range rows {
		sort.SliceStable(rows[i].words, func(a, b int) bool { return rows[i].words[a].x < rows[i].words[b].x })
	}
	return rows, nil
}
func (r cobitRow) practiceID(left, right float64) (string, int, bool, bool) {
	pattern := regexp.MustCompile(`^(EDM|APO|BAI|DSS|MEA)[0-9]{2}\.[0-9]{2}$`)
	for i, w := range r.words {
		if w.x >= left-1 && w.x < right-4 && strings.TrimSpace(w.text) != "" {
			return w.text, i, true, math.Abs(w.x-left) <= 1 && pattern.MatchString(w.text)
		}
	}
	return "", -1, false, false
}
