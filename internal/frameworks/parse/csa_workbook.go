package parse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type csaWorkbook struct {
	Sheets []struct {
		Name string `xml:"name,attr"`
		ID   string `xml:"id,attr"`
	} `xml:"sheets>sheet"`
}
type csaRelationships struct {
	Entries []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Mode   string `xml:"TargetMode,attr"`
		Type   string `xml:"Type,attr"`
	} `xml:"Relationship"`
}
type csaText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (t csaText) value() string {
	var b strings.Builder
	b.WriteString(t.Text)
	for _, run := range t.Runs {
		b.WriteString(run.Text)
	}
	return csaUnescape(b.String())
}

func csaUnescape(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+7 <= len(s) && s[i:i+2] == "_x" && s[i+6] == '_' {
			v, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
			if err == nil && s[i+2] != '+' {
				r := rune(v)
				i += 7
				if r >= 0xd800 && r <= 0xdbff && i+7 <= len(s) && s[i:i+2] == "_x" && s[i+6] == '_' {
					if low, e := strconv.ParseUint(s[i+2:i+6], 16, 16); e == nil && s[i+2] != '+' && low >= 0xdc00 && low <= 0xdfff {
						r = utf16.DecodeRune(r, rune(low))
						i += 7
					}
				}
				if !utf8.ValidRune(r) {
					r = utf8.RuneError
				}
				b.WriteRune(r)
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

type csaSheet struct {
	Rows []struct {
		Index int `xml:"r,attr"`
		Cells []struct {
			Ref    string    `xml:"r,attr"`
			Type   string    `xml:"t,attr"`
			Values []string  `xml:"v"`
			Inline []csaText `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
	Merges []struct {
		Ref string `xml:"ref,attr"`
	} `xml:"mergeCells>mergeCell"`
}

func csaZipXML(files map[string]*zip.File, name, root string, value any, budget *uint64) error {
	file := files[name]
	if file == nil || file.UncompressedSize64 > *budget {
		return fmt.Errorf("invalid workbook part")
	}
	*budget -= file.UncompressedSize64
	r, err := file.Open()
	if err != nil {
		return fmt.Errorf("invalid workbook part")
	}
	defer func() { _ = r.Close() }()
	decoder := xml.NewDecoder(io.LimitReader(r, (256<<20)+1))
	for {
		token, e := decoder.Token()
		if e != nil {
			return fmt.Errorf("invalid workbook XML")
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local != root || decoder.DecodeElement(value, &start) != nil {
				return fmt.Errorf("invalid workbook XML")
			}
			break
		}
		if text, ok := token.(xml.CharData); ok && strings.TrimSpace(string(text)) != "" {
			return fmt.Errorf("invalid workbook XML")
		}
	}
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("invalid workbook XML")
		}
		switch v := token.(type) {
		case xml.Comment:
		case xml.CharData:
			if strings.TrimSpace(string(v)) != "" {
				return fmt.Errorf("invalid workbook XML")
			}
		default:
			return fmt.Errorf("invalid workbook XML")
		}
	}
	return nil
}

func readCSAWorkbook(data []byte, trailer int) ([][]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid workbook")
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if files[file.Name] != nil {
			return nil, fmt.Errorf("duplicate workbook part")
		}
		files[file.Name] = file
	}
	budget := uint64(256 << 20)
	var wb csaWorkbook
	if err = csaZipXML(files, "xl/workbook.xml", "workbook", &wb, &budget); err != nil {
		return nil, err
	}
	id := ""
	for _, sheet := range wb.Sheets {
		if strings.EqualFold(sheet.Name, "CCM") && sheet.Name != "CCM" {
			return nil, fmt.Errorf("duplicate workbook sheet")
		}
		if sheet.Name == "CCM" {
			if id != "" {
				return nil, fmt.Errorf("duplicate workbook sheet")
			}
			id = sheet.ID
		}
	}
	if id == "" {
		return nil, fmt.Errorf("missing workbook sheet")
	}
	var rels csaRelationships
	if err = csaZipXML(files, "xl/_rels/workbook.xml.rels", "Relationships", &rels, &budget); err != nil {
		return nil, err
	}
	target := ""
	for _, rel := range rels.Entries {
		if rel.ID == id {
			if target != "" || rel.Mode != "" || !strings.HasSuffix(rel.Type, "/worksheet") {
				return nil, fmt.Errorf("invalid workbook sheet")
			}
			target = rel.Target
		}
	}
	if target == "" {
		return nil, fmt.Errorf("missing workbook sheet")
	}
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	} else {
		target = path.Join("xl", target)
	}
	if !strings.HasPrefix(target, "xl/") || path.Clean(target) != target {
		return nil, fmt.Errorf("invalid workbook sheet")
	}
	var sheet csaSheet
	if err = csaZipXML(files, target, "worksheet", &sheet, &budget); err != nil {
		return nil, err
	}
	type mergedRange struct{ c1, r1, c2, r2 int }
	var ranges []mergedRange
	for _, merge := range sheet.Merges {
		bounds := strings.Split(merge.Ref, ":")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		c1, r1, e1 := excelize.CellNameToCoordinates(bounds[0])
		c2, r2, e2 := excelize.CellNameToCoordinates(bounds[1])
		start, e3 := excelize.CoordinatesToCellName(c1, r1)
		end, e4 := excelize.CoordinatesToCellName(c2, r2)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || start != bounds[0] || end != bounds[1] || c1 > c2 || r1 > r2 || c2 > 16384 || r2 > 1048576 {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		ranges = append(ranges, mergedRange{c1, r1, c2, r2})
	}
	starts := map[int][]int{}
	ends := map[int][]int{}
	lastStart := 0
	for i, merged := range ranges {
		starts[merged.r1] = append(starts[merged.r1], i)
		ends[merged.r2+1] = append(ends[merged.r2+1], i)
		if merged.r1 > lastStart {
			lastStart = merged.r1
		}
	}
	active := make([]int, 16385)
	currentRow := 0
	advance := func(target int) error {
		for currentRow < target {
			currentRow++
			for _, id := range ends[currentRow] {
				merged := ranges[id]
				for col := merged.c1; col <= merged.c2; col++ {
					active[col] = 0
				}
			}
			for _, id := range starts[currentRow] {
				merged := ranges[id]
				for col := merged.c1; col <= merged.c2; col++ {
					if active[col] != 0 {
						return fmt.Errorf("invalid workbook merge")
					}
					active[col] = id + 1
				}
			}
		}
		return nil
	}
	var rows [][]string
	var sharedValues []string
	sharedLoaded := false
	var decodedBytes int64
	previousRow := 0
	for _, row := range sheet.Rows {
		if row.Index <= previousRow || row.Index > 1048576 {
			return nil, fmt.Errorf("invalid workbook cell")
		}
		previousRow = row.Index
		if err = advance(row.Index); err != nil {
			return nil, err
		}
		previousCol := 0
		for _, cell := range row.Cells {
			col, index, e := excelize.CellNameToCoordinates(cell.Ref)
			if e != nil || index != row.Index || col <= previousCol || col > 16384 {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			previousCol = col
			canonical, e := excelize.CoordinatesToCellName(col, index)
			if e != nil || canonical != cell.Ref || len(cell.Values) > 1 || len(cell.Inline) > 1 {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			value := ""
			switch cell.Type {
			case "inlineStr":
				if len(cell.Inline) != 1 || len(cell.Values) != 0 {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				value = cell.Inline[0].value()
			case "", "n", "str":
				if len(cell.Inline) != 0 || cell.Type != "" && len(cell.Values) != 1 {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				if len(cell.Values) == 1 {
					value = cell.Values[0]
				}
			case "b":
				if len(cell.Inline) != 0 || len(cell.Values) != 1 {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				switch cell.Values[0] {
				case "0":
					value = "FALSE"
				case "1":
					value = "TRUE"
				default:
					return nil, fmt.Errorf("invalid workbook cell")
				}
			case "s":
				if len(cell.Inline) != 0 || len(cell.Values) != 1 {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				if !sharedLoaded {
					var shared struct {
						Items []csaText `xml:"si"`
					}
					if e = csaZipXML(files, "xl/sharedStrings.xml", "sst", &shared, &budget); e != nil {
						return nil, e
					}
					sharedValues = make([]string, len(shared.Items))
					for i, item := range shared.Items {
						sharedValues[i] = item.value()
					}
					sharedLoaded = true
				}
				item, parseErr := strconv.ParseUint(strings.TrimSpace(cell.Values[0]), 10, 32)
				if parseErr != nil || item >= uint64(len(sharedValues)) {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				value = sharedValues[item]
			default:
				return nil, fmt.Errorf("invalid workbook cell")
			}
			decodedBytes += int64(len(value))
			if decodedBytes > 256<<20 {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			if id := active[col]; id != 0 {
				merged := ranges[id-1]
				if col != merged.c1 || index != merged.r1 {
					if csaField(value) != "" {
						return nil, fmt.Errorf("invalid workbook merge")
					}
					value = ""
				}
			}
			if trailer > 0 && index >= trailer {
				continue
			}
			if col > 23 {
				if index >= 2 && strings.TrimSpace(value) != "" {
					return nil, fmt.Errorf("invalid workbook cell")
				}
				continue
			}
			for len(rows) < index {
				rows = append(rows, nil)
			}
			for len(rows[index-1]) < col {
				rows[index-1] = append(rows[index-1], "")
			}
			rows[index-1][col-1] = value
		}
	}
	if err = advance(lastStart); err != nil {
		return nil, err
	}
	groups := map[string]bool{"F2:H2": false, "I2:N2": false, "O2:W2": false}
	merges := map[string]bool{}
	for _, merge := range sheet.Merges {
		bounds := strings.Split(merge.Ref, ":")
		if len(bounds) != 2 || merges[merge.Ref] {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		merges[merge.Ref] = true
		c1, r1, e1 := excelize.CellNameToCoordinates(bounds[0])
		c2, r2, e2 := excelize.CellNameToCoordinates(bounds[1])
		start, e3 := excelize.CoordinatesToCellName(c1, r1)
		end, e4 := excelize.CoordinatesToCellName(c2, r2)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || start != bounds[0] || end != bounds[1] || c1 > c2 || r1 > r2 {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		if _, ok := groups[merge.Ref]; ok {
			groups[merge.Ref] = true
			continue
		}
		if r1 == 1 && r2 == 1 || trailer > 0 && r1 >= trailer {
			continue
		}
		if r1 == r2 && r1 >= 4 && c1 == 1 && c2 == 5 && r1 <= len(rows) && csaField(cell(rows[r1-1], 0)) != "" && csaField(cell(rows[r1-1], 2)) == "" {
			continue
		}
		return nil, fmt.Errorf("invalid workbook merge")
	}
	for _, found := range groups {
		if !found {
			return nil, fmt.Errorf("invalid workbook merge")
		}
	}
	return rows, nil
}
