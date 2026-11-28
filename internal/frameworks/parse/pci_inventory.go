package parse

import (
	"fmt"
	"strconv"
	"strings"
)

func pciValidateInventory(nodes []Node, tables map[string]bool) error {
	rootCount := 0
	for _, n := range nodes {
		if n.Parent >= 0 {
			continue
		}
		rootCount++
		want := ""
		if rootCount <= 12 {
			want = strconv.Itoa(rootCount)
		} else if rootCount <= 15 {
			want = "A" + strconv.Itoa(rootCount-12)
		}
		if n.Citation != want || !tables[n.Citation] {
			return fmt.Errorf("missing required region")
		}
	}
	if rootCount != 15 || len(nodes) == 0 || !strings.HasPrefix(nodes[len(nodes)-1].Citation, "A3.") {
		return fmt.Errorf("missing required region")
	}
	return nil
}
