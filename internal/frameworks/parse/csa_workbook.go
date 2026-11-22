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
type csaSheet struct {
	Rows []struct {
		Index int `xml:"r,attr"`
		Cells []struct {
			Ref   string `xml:"r,attr"`
			Type  string `xml:"t,attr"`
			Value string `xml:"v"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
	Merges []struct {
		Ref string `xml:"ref,attr"`
	} `xml:"mergeCells>mergeCell"`
}

func csaZipXML(files map[string]*zip.File, name string, value any) error {
	file := files[name]
	if file == nil || file.UncompressedSize64 > 256<<20 {
		return fmt.Errorf("invalid workbook part")
	}
	r, err := file.Open()
	if err != nil {
		return fmt.Errorf("invalid workbook part")
	}
	defer func() { _ = r.Close() }()
	if err = xml.NewDecoder(io.LimitReader(r, (256<<20)+1)).Decode(value); err != nil {
		return fmt.Errorf("invalid workbook XML")
	}
	return nil
}

func readCSAWorkbook(data []byte, trailer int) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 256 << 20, UnzipXMLSizeLimit: 256 << 20})
	if err != nil {
		return nil, fmt.Errorf("invalid workbook")
	}
	defer func() { _ = f.Close() }()
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
	var wb csaWorkbook
	if err = csaZipXML(files, "xl/workbook.xml", &wb); err != nil {
		return nil, err
	}
	id := ""
	for _, sheet := range wb.Sheets {
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
	if err = csaZipXML(files, "xl/_rels/workbook.xml.rels", &rels); err != nil {
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
	if err = csaZipXML(files, target, &sheet); err != nil {
		return nil, err
	}
	needed := map[string]bool{"F2:H2": false, "I2:N2": false, "O2:W2": false}
	for _, merge := range sheet.Merges {
		bounds := strings.Split(merge.Ref, ":")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		c1, r1, e1 := excelize.CellNameToCoordinates(bounds[0])
		c2, r2, e2 := excelize.CellNameToCoordinates(bounds[1])
		if e1 != nil || e2 != nil || c1 > c2 || r1 > r2 {
			return nil, fmt.Errorf("invalid workbook merge")
		}
		if r1 <= 2 && r2 >= 2 {
			if _, ok := needed[merge.Ref]; !ok || needed[merge.Ref] {
				return nil, fmt.Errorf("invalid workbook merge")
			}
			needed[merge.Ref] = true
		} else if r2 >= 3 && (trailer == 0 || r1 < trailer) {
			return nil, fmt.Errorf("invalid workbook merge")
		}
	}
	for _, ok := range needed {
		if !ok {
			return nil, fmt.Errorf("invalid workbook merge")
		}
	}
	seen := map[string]bool{}
	var rows [][]string
	var shared struct {
		Items []struct{} `xml:"si"`
	}
	sharedLoaded := false
	for _, row := range sheet.Rows {
		if trailer > 0 && row.Index >= trailer {
			continue
		}
		for _, cell := range row.Cells {
			col, index, e := excelize.CellNameToCoordinates(cell.Ref)
			if e != nil || index != row.Index || seen[cell.Ref] {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			canonical, e := excelize.CoordinatesToCellName(col, index)
			if e != nil || canonical != cell.Ref {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			seen[cell.Ref] = true
			if trailer > 0 && index >= trailer {
				continue
			}
			if cell.Type == "s" {
				if !sharedLoaded {
					if e = csaZipXML(files, "xl/sharedStrings.xml", &shared); e != nil {
						return nil, e
					}
					sharedLoaded = true
				}
				item, parseErr := strconv.ParseUint(strings.TrimSpace(cell.Value), 10, 32)
				if parseErr != nil || item >= uint64(len(shared.Items)) {
					return nil, fmt.Errorf("invalid workbook cell")
				}
			}
			value, e := f.GetCellValue("CCM", cell.Ref)
			if e != nil {
				return nil, fmt.Errorf("invalid workbook cell")
			}
			if index >= 2 && index <= 3 && col > 23 && strings.TrimSpace(value) != "" {
				return nil, fmt.Errorf("invalid workbook header")
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
	return rows, nil
}
