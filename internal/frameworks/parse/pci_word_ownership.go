package parse

import (
	"fmt"
	"sort"
)

type pciWordOpener struct {
	key   pciKey
	owner int
}

func pciValidateWordOwnership(lines []pciLine, topRow int) error {
	var openers []pciWordOpener
	for _, line := range lines {
		if line.role != pciRoot && line.role != pciLineGroup && line.role != pciLineRow {
			continue
		}
		if line.owner < 0 {
			return fmt.Errorf("missing ownership anchor")
		}
		anchor := line.citation
		if line.role == pciRoot {
			anchor = line.secondText
		}
		found := false
		for i, word := range line.retained {
			if !word.emit && word.text == anchor {
				openers = append(openers, pciWordOpener{key: pciOrderKey(line, i), owner: line.owner})
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing ownership anchor")
		}
	}
	sort.Slice(openers, func(i, j int) bool { return pciKeyLess(openers[i].key, openers[j].key) })
	indices := make(map[int]int, len(openers))
	for i, opener := range openers {
		if _, ok := indices[opener.owner]; ok {
			return fmt.Errorf("ambiguous word ownership")
		}
		indices[opener.owner] = i
	}
	for _, line := range lines {
		if line.owner < 0 {
			continue
		}
		ownerIndex, ok := indices[line.owner]
		if !ok {
			if line.owner != topRow {
				return fmt.Errorf("ambiguous word ownership")
			}
			ownerIndex = -1
		}
		nextOpener := ownerIndex + 1
		for wi, word := range line.retained {
			if !word.emit {
				continue
			}
			if nextOpener < len(openers) && word.box.ymin >= openers[nextOpener].key.y {
				return fmt.Errorf("ambiguous word ownership")
			}
			wordKey := pciOrderKey(line, wi)
			next := sort.Search(len(openers), func(i int) bool { return !pciKeyLess(openers[i].key, wordKey) })
			owner := topRow
			if next > 0 {
				owner = openers[next-1].owner
			}
			if owner != line.owner {
				return fmt.Errorf("ambiguous word ownership")
			}
		}
	}
	return nil
}
