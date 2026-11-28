package parse

import (
	"fmt"
	"strings"
)

type pciCaptionResult struct {
	found, standalone bool
	row               int
}

func pciValidateStandalone(seen map[int]bool) error {
	if len(seen) != 2 || !seen[96] || !seen[98] {
		return fmt.Errorf("missing standalone captions")
	}
	return nil
}
func pciCaption(line pciLine, block []pciLine, h pciHeader, state *pciNodes, topRow int, opened bool, page int, seen map[int]bool) (pciCaptionResult, error) {
	var result pciCaptionResult
	if line.role == pciLineStandalone {
		result.found = true
		result.standalone = true
		result.row = -1
		if (page != 96 && page != 98) || seen[page] || h.paired || opened || topRow < 0 || state.activeRow != topRow {
			return result, fmt.Errorf("invalid standalone caption")
		}
		seen[page] = true
		return result, nil
	}
	if line.role != pciLineCaption {
		return result, nil
	}
	result.found = true
	result.row = -1
	word := line.citation
	idx, ok := state.seen[word]
	if !ok || !state.rows[word] || !strings.HasPrefix(word, state.root+".") {
		return result, fmt.Errorf("unknown continuation")
	}
	hasLeft := false
	for _, part := range block {
		if pciHasContent(part) {
			hasLeft = true
		}
	}
	if hasLeft {
		if idx != state.activeRow {
			return result, fmt.Errorf("wrong continuation")
		}
		result.row = idx
	}
	return result, nil
}
