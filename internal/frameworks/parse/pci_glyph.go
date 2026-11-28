package parse

import (
	"fmt"
	"sort"
)

type pciFenwick []int

func (f pciFenwick) add(i, delta int) {
	for i++; i < len(f); i += i & -i {
		f[i] += delta
	}
}
func (f pciFenwick) sum(i int) int {
	n := 0
	for ; i > 0; i -= i & -i {
		n += f[i]
	}
	return n
}
func (f pciFenwick) kth(n int) int {
	i := 0
	bit := 1
	for bit<<1 < len(f) {
		bit <<= 1
	}
	for ; bit > 0; bit >>= 1 {
		j := i + bit
		if j < len(f) && f[j] < n {
			i = j
			n -= f[j]
		}
	}
	return i
}

func pciJoinGlyphs(lines []pciBodyLine) ([]pciBodyLine, error) {
	out := make([]pciBodyLine, 0, len(lines))
	for start := 0; start < len(lines); {
		end := start + 1
		for end < len(lines) && lines[end].page == lines[start].page {
			end++
		}
		indices := make([]int, 0, end-start)
		byY := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			byY = append(byY, i)
			if !pciGlyph(lines[i].text) {
				indices = append(indices, i)
			}
		}
		sort.SliceStable(byY, func(i, j int) bool {
			a, b := lines[byY[i]].geometry, lines[byY[j]].geometry
			if a.ymin != b.ymin {
				return a.ymin < b.ymin
			}
			return a.xmin < b.xmin
		})
		sort.Slice(indices, func(i, j int) bool {
			a, b := lines[indices[i]], lines[indices[j]]
			if a.geometry.xmin != b.geometry.xmin {
				return a.geometry.xmin < b.geometry.xmin
			}
			return indices[i] < indices[j]
		})
		ranks := make(map[int]int, len(indices))
		for rank, i := range indices {
			ranks[i] = rank
		}
		tree := make(pciFenwick, len(indices)+1)
		low, high := 0, 0
		targets := make(map[int]string)
		glyphs := make(map[int]bool)
		for _, i := range byY {
			g := lines[i]
			if !pciGlyph(g.text) {
				continue
			}
			for high < len(byY) && lines[byY[high]].geometry.ymin <= g.geometry.ymin+2 {
				if rank, ok := ranks[byY[high]]; ok {
					tree.add(rank, 1)
				}
				high++
			}
			for low < len(byY) && lines[byY[low]].geometry.ymin < g.geometry.ymin-2 {
				if rank, ok := ranks[byY[low]]; ok {
					tree.add(rank, -1)
				}
				low++
			}
			boundary := sort.Search(len(indices), func(j int) bool { return lines[indices[j]].geometry.xmin > g.geometry.xmin })
			before := tree.sum(boundary)
			count := tree.sum(len(indices)) - before
			if count != 1 {
				return nil, fmt.Errorf("ambiguous list glyph")
			}
			target := indices[tree.kth(before+1)]
			if _, ok := targets[target]; ok {
				return nil, fmt.Errorf("ambiguous list glyph")
			}
			targets[target] = g.text
			glyphs[i] = true
		}
		for i := start; i < end; i++ {
			if glyphs[i] {
				continue
			}
			line := lines[i]
			if glyph, ok := targets[i]; ok {
				line.text = glyph + " " + line.text
			}
			out = append(out, line)
		}
		start = end
	}
	return out, nil
}
