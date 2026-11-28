package parse

import (
	"fmt"

	"danny.vn/trachuan/internal/pdftext"
)

const pciSHA = "5e6b9093b84007b973097d20126a3768ea2f0a1d4200255c849b0fb3bf04ebc7"

func parsePCIDSS(pages []pdftext.Page, sourceSHA string, expected *int) (*Result, error) {
	return parsePCIProfile(pages, sourceSHA, expected, true)
}

func parsePCIProfile(pages []pdftext.Page, sourceSHA string, expected *int, strict bool) (*Result, error) {
	if sourceSHA != pciSHA {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	if len(pages) < 372 {
		return nil, fmt.Errorf("missing required region")
	}
	total := 0
	add := func(s string) bool {
		if len(s) > 50_000_000-total {
			return false
		}
		total += len(s)
		return true
	}
	addWord := func(w pdftext.Word) bool {
		return add(w.Text) && add(w.XMin) && add(w.XMax) && add(w.YMin) && add(w.YMax)
	}
	for _, p := range pages {
		if !add(p.Width) || !add(p.Height) {
			return nil, fmt.Errorf("input limit exceeded")
		}
		for _, b := range p.Blocks {
			if !add(b.XMin) || !add(b.XMax) || !add(b.YMin) || !add(b.YMax) {
				return nil, fmt.Errorf("input limit exceeded")
			}
			for _, l := range b.Lines {
				if !add(l.XMin) || !add(l.XMax) || !add(l.YMin) || !add(l.YMax) {
					return nil, fmt.Errorf("input limit exceeded")
				}
				for _, w := range l.Words {
					if !addWord(w) {
						return nil, fmt.Errorf("input limit exceeded")
					}
				}
			}
		}
	}
	nodes, err := walkPCI(pages, strict)
	if err != nil {
		return nil, err
	}
	return finish(nodes, nil, expected)
}
