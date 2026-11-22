package build

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/frameworks/relate"
)

type titleCatalog struct {
	display  map[relate.Key]string
	licensed map[relate.Key]bool
}

func titleKey(code, version, citation string) relate.Key {
	return relate.Key{Code: code, Version: version, Citation: strings.ToUpper(strings.ReplaceAll(citation, " ", ""))}
}

func newTitleCatalog() *titleCatalog {
	return &titleCatalog{display: map[relate.Key]string{}, licensed: map[relate.Key]bool{}}
}

func (c *titleCatalog) add(v registry.Framework, nodes []parse.Node) {
	for _, n := range nodes {
		k := titleKey(v.Code, v.Version, n.Citation)
		c.display[k] = n.Citation
		if v.Access == "licensed" {
			c.licensed[k] = true
		}
	}
}

func (c *titleCatalog) validate(rows map[relate.Key]string, coverage bool) (map[relate.Key]string, error) {
	matched := map[relate.Key]string{}
	seen := map[relate.Key]bool{}
	for key, title := range rows {
		k := titleKey(key.Code, key.Version, key.Citation)
		display, ok := c.display[k]
		if !ok || seen[k] || strings.TrimSpace(title) == "" || !utf8.ValidString(title) || strings.ContainsRune(title, 0) {
			return nil, fmt.Errorf("invalid title row")
		}
		seen[k] = true
		matched[relate.Key{Code: k.Code, Version: k.Version, Citation: display}] = title
	}
	if coverage {
		for k := range c.licensed {
			if !seen[k] {
				return nil, fmt.Errorf("missing title coverage")
			}
		}
	}
	return matched, nil
}
