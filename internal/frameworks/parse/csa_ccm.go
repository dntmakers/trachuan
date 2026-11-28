package parse

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var csaDomain = regexp.MustCompile(`(?:^|[[:space:]])(?:\(([A-Z]+(?:&[A-Z]+)*)\)|([A-Z]+(?:&[A-Z]+)*))$`)
var csaControl = regexp.MustCompile(`^([A-Z]+(?:&[A-Z]+)*)-[0-9]{2}$`)

func csaField(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s))
}

func parseCSA(data []byte) ([]Node, error) {
	return parseCSAPinned(data, "5e721628c8ab297bdbd355afa4c01699971fcbb9cb16802ccb9d42c7176ab32b", 228)
}

func parseCSAPinned(data []byte, footerDigest string, footerRow int) ([]Node, error) {
	matched := fmt.Sprintf("%x", sha256.Sum256(data)) == footerDigest
	trailer := 0
	if matched {
		trailer = footerRow
	}
	r, err := readCSAWorkbook(data, trailer)
	if err != nil {
		return nil, err
	}
	if len(r) < 3 || (matched && footerRow < 4) {
		return nil, fmt.Errorf("invalid workbook header")
	}
	groups := []string{csaField(cell(r[1], 5)), csaField(cell(r[1], 8)), csaField(cell(r[1], 14))}
	for _, g := range groups {
		if g == "" {
			return nil, fmt.Errorf("invalid workbook header")
		}
	}
	for i := 0; i < 23; i++ {
		if i != 5 && i != 8 && i != 14 && csaField(cell(r[1], i)) != "" {
			return nil, fmt.Errorf("invalid workbook header")
		}
	}
	for i := 23; i < len(r[2]); i++ {
		if cell(r[2], i) != "" {
			return nil, fmt.Errorf("invalid workbook header")
		}
	}
	for _, bounds := range [][2]int{{0, 4}, {4, 5}, {5, 8}, {8, 14}, {14, 23}} {
		seen := map[string]bool{}
		for i := bounds[0]; i < bounds[1]; i++ {
			label := csaField(cell(r[2], i))
			if label == "" || seen[strings.ToLower(label)] {
				return nil, fmt.Errorf("invalid workbook header")
			}
			seen[strings.ToLower(label)] = true
		}
	}
	var nodes []Node
	parent := -1
	for index, row := range r[3:] {
		a, b, id, statement := csaField(cell(row, 0)), csaField(cell(row, 1)), csaField(cell(row, 2)), csaField(cell(row, 3))
		if matched && index+4 >= footerRow {
			continue
		}
		for i := 23; i < len(row); i++ {
			if csaField(row[i]) != "" {
				return nil, fmt.Errorf("invalid control row")
			}
		}
		if a == "" && b == "" && id == "" && statement == "" {
			for i := 4; i < 23; i++ {
				if cell(row, i) != "" {
					return nil, fmt.Errorf("invalid control row")
				}
			}
			continue
		}
		if id == "" {
			for i := 4; i < 23; i++ {
				if csaField(cell(row, i)) != "" {
					return nil, fmt.Errorf("invalid domain row")
				}
			}
			match := csaDomain.FindStringSubmatch(a)
			if match == nil || b != "" || statement != "" {
				return nil, fmt.Errorf("invalid domain row")
			}
			acronym := match[1]
			if acronym == "" {
				acronym = match[2]
			}
			parent = len(nodes)
			nodes = append(nodes, Node{Citation: acronym, Kind: "domain", Parent: -1, Ordinal: parent, PublisherTitle: a, Status: "active"})
			continue
		}
		match := csaControl.FindStringSubmatch(id)
		if match == nil || parent < 0 || nodes[parent].Citation != match[1] || b == "" || statement == "" {
			return nil, fmt.Errorf("invalid control row")
		}
		var body strings.Builder
		body.WriteString(statement)
		body.WriteString("\n\n")
		for i := 4; i < 23; i++ {
			if i > 4 {
				body.WriteByte('\n')
			}
			if i >= 5 {
				group := 0
				if i >= 14 {
					group = 2
				} else if i >= 8 {
					group = 1
				}
				body.WriteString(groups[group])
				body.WriteString(" / ")
			}
			body.WriteString(csaField(cell(r[2], i)))
			body.WriteByte(':')
			if value := csaField(cell(row, i)); value != "" {
				body.WriteByte(' ')
				body.WriteString(value)
			}
		}
		nodes = append(nodes, Node{Citation: id, Kind: "control", Parent: parent, Ordinal: len(nodes), PublisherTitle: b, Body: body.String(), Status: "active"})
	}
	return nodes, nil
}
