package parse

import (
	"fmt"
	"strconv"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
)

func pciHeadingTitle(block []pciLine) string {
	var words []string
	for _, line := range block {
		switch line.role {
		case pciRoot, pciLineHeadingPart, pciLineGroup, pciLineGroupPart:
			for _, w := range line.retained {
				if !w.emit {
					continue
				}
				words = append(words, w.text)
			}
		}
	}
	return strings.Join(words, " ")
}
func pciRootID(line pciLine) (string, error) {
	id := strings.TrimSuffix(line.secondText, ":")
	if line.firstText == "Requirement" {
		n, err := strconv.Atoi(id)
		if err != nil || n < 1 || n > 12 || strconv.Itoa(n) != id {
			return "", fmt.Errorf("invalid root")
		}
	} else if id != "A1" && id != "A2" && id != "A3" {
		return "", fmt.Errorf("invalid root")
	}
	return id, nil
}
func walkPCI(pages []pdftext.Page, strict bool) ([]Node, error) {
	state := pciNodes{active: -1, activeRow: -1}
	previous := make(map[[2]float64]pciHeader)
	bodies := make(map[int]*pciBody)
	standalone := make(map[int]bool)
	tables := make(map[string]bool)
	lastA3Table := 0
	for pageIndex, page := range pages[41:372] {
		lines, err := pciClassifyPage(page)
		if err != nil {
			return nil, err
		}
		h, table, err := pciHeaderForLines(lines, previous)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", pageIndex+42, err)
		}
		if strict {
			table, err = pciPageKind(pageIndex+42, table)
			if err != nil {
				return nil, fmt.Errorf("page %d: %w", pageIndex+42, err)
			}
		}
		if err = pciClassifyTable(lines, h, table, pageIndex+42); err != nil {
			return nil, fmt.Errorf("page %d: %w", pageIndex+42, err)
		}
		pciOrderProjectedLines(lines)
		if table {
			if err = pciRejectCrossingOpeners(lines, h); err != nil {
				return nil, fmt.Errorf("page %d: %w", pageIndex+42, err)
			}
		}
		byBlock := make(map[int][]pciLine)
		blockIndices := make(map[int][]int)
		for i, line := range lines {
			byBlock[line.block] = append(byBlock[line.block], line)
			blockIndices[line.block] = append(blockIndices[line.block], i)
		}
		topRow, opened, pending := state.activeRow, false, false
		headingBlocks := map[int]bool{}
		record := func() {
			if state.root != "" {
				tables[state.root] = true
			}
			if state.root == "A3" {
				lastA3Table = pageIndex + 42
			}
		}
		for lineIndex := range lines {
			line := lines[lineIndex]
			if headingBlocks[line.block] {
				continue
			}
			if line.role == pciRoot && line.first {
				if pending {
					return nil, fmt.Errorf("missing standalone continuation")
				}
				id, rootErr := pciRootID(line)
				if rootErr != nil {
					return nil, rootErr
				}
				title := pciHeadingTitle(byBlock[line.block])
				if addErr := state.add(id, title, true); addErr != nil {
					return nil, addErr
				}
				for _, i := range blockIndices[line.block] {
					if lines[i].role == pciRoot || lines[i].role == pciLineHeadingPart {
						lines[i].owner = state.active
					}
				}
				state.activeRow = -1
				opened = true
				headingBlocks[line.block] = true
				continue
			}
			if line.role == pciLineHeadingPart || line.role == pciLineGroupPart {
				return nil, fmt.Errorf("ambiguous line ownership")
			}
			if !table || pciStructural(line.role) || line.role == pciLineIgnore {
				continue
			}
			switch line.role {
			case pciLineStandalone, pciLineCaption:
				caption, captionErr := pciCaption(line, byBlock[line.block], h, &state, topRow, opened, pageIndex+42, standalone)
				if captionErr != nil {
					return nil, fmt.Errorf("page %d: %w", pageIndex+42, captionErr)
				}
				if caption.standalone {
					pending = true
				}
				if caption.row >= 0 {
					lines[lineIndex].owner = caption.row
					added := pciHasContent(line)
					if added {
						record()
						if caption.row == topRow {
							pending = false
						}
					}
				}
				continue
			case pciLineGroup, pciLineRow:
				if pending {
					return nil, fmt.Errorf("missing standalone continuation")
				}
				opened = true
				word := line.citation
				title := ""
				if line.role == pciLineGroup {
					title = pciHeadingTitle(byBlock[line.block])
					headingBlocks[line.block] = true
				}
				if addErr := state.add(word, title, false); addErr != nil {
					return nil, addErr
				}
				if line.role == pciLineGroup {
					for _, i := range blockIndices[line.block] {
						if lines[i].role == pciLineGroup || lines[i].role == pciLineGroupPart {
							lines[i].owner = state.active
						}
					}
					state.activeRow = -1
				} else {
					state.activeRow = state.active
					state.rows[word] = true
					bodies[state.activeRow] = &pciBody{}
					lines[lineIndex].owner = state.activeRow
					record()
				}
				continue
			}
			if line.role != pciLineBody {
				return nil, fmt.Errorf("undecided line role")
			}
			hasLeft := pciHasContent(line)
			if !hasLeft {
				continue
			}
			if state.activeRow < 0 {
				return nil, fmt.Errorf("unowned table text at page %d", pageIndex+42)
			}
			lines[lineIndex].owner = state.activeRow
			record()
			if state.activeRow == topRow {
				pending = false
			}
		}
		if pending {
			return nil, fmt.Errorf("missing standalone continuation")
		}
		if err = pciValidateWordOwnership(lines, topRow); err != nil {
			return nil, fmt.Errorf("page %d: %w", pageIndex+42, err)
		}
		for _, line := range lines {
			if (line.role == pciLineBody || line.role == pciLineRow || (line.role == pciLineCaption && pciHasContent(line))) && line.owner < 0 {
				return nil, fmt.Errorf("undecided line owner")
			}
			if line.owner < 0 || (line.role != pciLineBody && line.role != pciLineRow && line.role != pciLineCaption) {
				continue
			}
			bodies[line.owner].add(line, pageIndex)
		}
		if table {
			previous[[2]float64{h.outerLeft, h.outerRight}] = h
		}
	}
	if strict {
		if err := pciValidateInventory(state.nodes, tables); err != nil {
			return nil, err
		}
		if lastA3Table != 372 {
			return nil, fmt.Errorf("missing required region")
		}
		if err := pciValidateStandalone(standalone); err != nil {
			return nil, err
		}
	}
	if len(state.nodes) == 0 {
		return nil, fmt.Errorf("missing required region")
	}
	for i, body := range bodies {
		text, err := body.text()
		if err != nil {
			return nil, err
		}
		state.nodes[i].Body = text
	}
	return state.nodes, nil
}
