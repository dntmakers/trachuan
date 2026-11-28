package parse

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func mutateCSAWorksheet(t *testing.T, data []byte, mutate func(string) string) []byte {
	return mutateCSAPart(t, data, "xl/worksheets/sheet1.xml", mutate)
}

func mutateCSAPart(t *testing.T, data []byte, part string, mutate func(string) string) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, file := range r.File {
		src, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(src)
		_ = src.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == part {
			changed := mutate(string(content))
			if changed == string(content) {
				t.Fatal("worksheet mutation did not apply")
			}
			content = []byte(changed)
		}
		dst, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = dst.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCSAStrictReader(t *testing.T) {
	base := csaFixture(t, nil)
	for _, tc := range []struct {
		name   string
		change func(string) string
	}{
		{"duplicate merge", func(s string) string {
			return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="F2:H2"/></mergeCells>`, 1)
		}},
		{"overlapping merges", func(s string) string {
			return strings.Replace(s, `ref="F2:H2"`, `ref="F2:G2"/><mergeCell ref="G2:H2"`, 1)
		}},
		{"merged control rows", func(s string) string {
			return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="F5:F6"/></mergeCells>`, 1)
		}},
		{"invalid shared string", func(s string) string { return strings.Replace(s, `r="G5"`, `r="G5"`, 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := base
			if tc.name == "invalid shared string" {
				data = csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "G5", "invented") })
				tc.change = func(s string) string {
					return strings.Replace(s, `<c r="G5" t="s"><v>`, `<c r="G5" t="s"><v>999999`, 1)
				}
			}
			data = mutateCSAWorksheet(t, data, tc.change)
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted invalid worksheet")
			}
		})
	}
}

func csaInlineMergedFixture(t *testing.T, innerValue string) []byte {
	t.Helper()
	return mutateCSAWorksheet(t, csaFixture(t, nil), func(s string) string {
		for _, row := range []int{2, 4} {
			pattern := regexp.MustCompile(fmt.Sprintf(`(<row r="%d"[^>]*>)(.*?)(</row>)`, row))
			s = pattern.ReplaceAllStringFunc(s, func(text string) string {
				if row == 4 {
					cellPattern := regexp.MustCompile(`<c r="A4"[^>]*>.*?</c>`)
					text = cellPattern.ReplaceAllString(text, `<c r="A4" t="inlineStr"><is><t>Example Domain (EX)</t></is></c>`)
				}
				start, end := 2, 23
				if row == 2 {
					start, end = 7, 8
				}
				for col := start; col <= end; col++ {
					address, _ := excelize.CoordinatesToCellName(col, row)
					cellPattern := regexp.MustCompile(`<c r="` + address + `"[^>]*(?:/>|>.*?</c>)`)
					text = cellPattern.ReplaceAllString(text, "")
					value := ""
					if address == "C4" {
						value = innerValue
					}
					entry := `<c r="` + address + `" t="inlineStr"><is><t>` + value + `</t></is></c>`
					if row == 2 {
						text = strings.Replace(text, `<c r="I2"`, entry+`<c r="I2"`, 1)
					} else {
						text = strings.Replace(text, `</row>`, entry+`</row>`, 1)
					}
				}
				return text
			})
		}
		return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="A4:E4"/></mergeCells>`, 1)
	})
}

func TestCSAMergedInlineCells(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{{"empty", "", true}, {"nonblank", "invented-private-sentinel", false}} {
		t.Run(tc.name, func(t *testing.T) {
			expected := 2
			_, err := Parse("csa-ccm-4-1-xlsx", csaInlineMergedFixture(t, tc.value), "invented", "1", &expected)
			if tc.want && err != nil || !tc.want && (err == nil || err.Error() != "invalid workbook merge") {
				t.Fatalf("success = %v, want %v: %v", err == nil, tc.want, err)
			}
		})
	}
}

func TestCSAStrictCellTypes(t *testing.T) {
	base := csaFixture(t, nil)
	for _, tc := range []struct{ name, replacement string }{
		{"inline with value", `<c r="D5" t="inlineStr"><is><t>invented</t></is><v>7</v></c>`},
		{"unknown type", `<c r="D5" t="unknown"><v>invented</v></c>`},
		{"missing shared index", `<c r="D5" t="s"></c>`},
		{"numeric with inline", `<c r="D5" t="n"><is><t>invented</t></is><v>1</v></c>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateCSAWorksheet(t, base, func(s string) string {
				return regexp.MustCompile(`<c r="D5"[^>]*>.*?</c>`).ReplaceAllString(s, tc.replacement)
			})
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted invalid cell")
			}
		})
	}
}

func TestCSAOoxmlEscapes(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "D5", "UNIQUE_ESCAPE_MARKER") })
	data = mutateCSAPart(t, data, "xl/sharedStrings.xml", func(s string) string {
		return strings.Replace(s, "UNIQUE_ESCAPE_MARKER", "First_x000D_Second_x005F_x000A_", 1)
	})
	expected := 2
	result, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Nodes[1].Body, "First Second_x000A_\n\n") {
		t.Fatalf("unexpected escape result: %q", result.Nodes[1].Body)
	}
	data = mutateCSAWorksheet(t, data, func(s string) string {
		return regexp.MustCompile(`<c r="D5"[^>]*>.*?</c>`).ReplaceAllString(s, `<c r="D5" t="inlineStr"><is><t>Inline_x000D_Text</t></is></c>`)
	})
	result, err = Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil || !strings.HasPrefix(result.Nodes[1].Body, "Inline Text\n\n") {
		t.Fatalf("inline escape result: %v", err)
	}
}

func TestCSARejectsTrailingXMLAndWrongRoot(t *testing.T) {
	for _, tc := range []struct {
		name, part string
		change     func(string) string
	}{
		{"trailing sheet", "xl/worksheets/sheet1.xml", func(s string) string { return s + `<unexpected/>` }},
		{"wrong workbook root", "xl/workbook.xml", func(s string) string {
			return strings.ReplaceAll(strings.ReplaceAll(s, `<workbook `, `<wrong `), `</workbook>`, `</wrong>`)
		}},
		{"trailing shared", "xl/sharedStrings.xml", func(s string) string { return s + `invented trailing text` }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateCSAPart(t, csaFixture(t, nil), tc.part, tc.change)
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted invalid XML")
			}
		})
	}
}

func TestCSAWorkbookPartsShareBudget(t *testing.T) {
	data := csaFixture(t, nil)
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]*zip.File)
	for _, f := range r.File {
		files[f.Name] = f
	}
	workbook := files["xl/workbook.xml"]
	rels := files["xl/_rels/workbook.xml.rels"]
	if workbook == nil || rels == nil {
		t.Fatal("fixture parts missing")
	}
	budget := workbook.UncompressedSize64 + rels.UncompressedSize64 - 1
	var wb csaWorkbook
	if err := csaZipXML(files, workbook.Name, "workbook", &wb, &budget); err != nil {
		t.Fatal(err)
	}
	var relationships csaRelationships
	if err := csaZipXML(files, rels.Name, "Relationships", &relationships, &budget); err == nil {
		t.Fatal("accepted parts exceeding shared budget")
	}
	budget = rels.UncompressedSize64
	if err := csaZipXML(files, rels.Name, "Relationships", &relationships, &budget); err != nil {
		t.Fatalf("part rejected within budget: %v", err)
	}
}

func TestCSARejectsBadWorksheetCRC(t *testing.T) {
	data := csaFixture(t, nil)
	found := false
	for offset := 0; offset+46 <= len(data); offset++ {
		if string(data[offset:offset+4]) != "PK\x01\x02" {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(data[offset+28 : offset+30]))
		if offset+46+nameLen <= len(data) && string(data[offset+46:offset+46+nameLen]) == "xl/worksheets/sheet1.xml" {
			data[offset+16] ^= 1
			found = true
			break
		}
	}
	if !found {
		t.Fatal("worksheet zip entry missing")
	}
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted invalid worksheet CRC")
	}
}

func TestCSASharedExpansionLimit(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "D5", "EXPANSION_MARKER") })
	data = mutateCSAPart(t, data, "xl/sharedStrings.xml", func(s string) string {
		return strings.Replace(s, "EXPANSION_MARKER", strings.Repeat("a", 1<<20), 1)
	})
	data = mutateCSAWorksheet(t, data, func(s string) string {
		match := regexp.MustCompile(`<c r="D5" t="s"><v>([0-9]+)</v></c>`).FindStringSubmatch(s)
		if len(match) != 2 {
			t.Fatal("shared string reference missing")
		}
		var rows strings.Builder
		for row := 6; row <= 18; row++ {
			fmt.Fprintf(&rows, `<row r="%d">`, row)
			for col := 1; col <= 23; col++ {
				address, _ := excelize.CoordinatesToCellName(col, row)
				fmt.Fprintf(&rows, `<c r="%s" t="s"><v>%s</v></c>`, address, match[1])
			}
			rows.WriteString(`</row>`)
		}
		return strings.Replace(s, `</sheetData>`, rows.String()+`</sheetData>`, 1)
	})
	if _, err := readCSAWorkbook(data, 0); err == nil {
		t.Fatal("accepted expanded shared strings beyond limit")
	}
}

func TestCSAMergeAcrossRows(t *testing.T) {
	data := mutateCSAWorksheet(t, csaFixture(t, func(f *excelize.File) {
		_ = f.SetCellValue("CCM", "A228", "trailer")
		_ = f.SetCellValue("CCM", "B229", "invented sentinel")
	}), func(s string) string {
		return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="A228:E229"/></mergeCells>`, 1)
	})
	if _, err := parseCSAPinned(data, fmt.Sprintf("%x", sha256.Sum256(data)), 228); err == nil || err.Error() != "invalid workbook merge" {
		t.Fatalf("expected invalid workbook merge, got %v", err)
	}
}

func TestCSAAllInlineWithoutSharedStrings(t *testing.T) {
	data := mutateCSAWorksheet(t, csaFixture(t, nil), func(s string) string {
		var sheet strings.Builder
		sheet.WriteString(`<sheetData><row r="2"><c r="F2" t="inlineStr"><is><t>Models</t></is></c><c r="I2" t="inlineStr"><is><t>Architecture</t></is></c><c r="O2" t="inlineStr"><is><t>Organization</t></is></c></row><row r="3">`)
		for col := 1; col <= 23; col++ {
			address, _ := excelize.CoordinatesToCellName(col, 3)
			fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>Column %02d</t></is></c>`, address, col)
		}
		sheet.WriteString(`</row><row r="4"><c r="A4" t="inlineStr"><is><t>Example Domain (EX)</t></is></c></row><row r="5"><c r="B5" t="inlineStr"><is><t>Invented title</t></is></c><c r="C5" t="inlineStr"><is><t>EX-01</t></is></c><c r="D5" t="inlineStr"><is><t>Invented statement</t></is></c></row></sheetData>`)
		return regexp.MustCompile(`<sheetData>.*?</sheetData>`).ReplaceAllString(s, sheet.String())
	})
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	removed := false
	for _, file := range r.File {
		if file.Name == "xl/sharedStrings.xml" {
			removed = true
			continue
		}
		src, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		dst, createErr := w.Create(file.Name)
		if createErr != nil {
			_ = src.Close()
			t.Fatal(createErr)
		}
		_, err = io.Copy(dst, src)
		_ = src.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !removed {
		t.Fatal("fixture did not contain shared strings")
	}
	expected := 2
	result, err := Parse("csa-ccm-4-1-xlsx", out.Bytes(), "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Nodes[1].Citation != "EX-01" || !strings.HasPrefix(result.Nodes[1].Body, "Invented statement\n\n") {
		t.Fatalf("unexpected inline result: %+v", result.Nodes)
	}
}

func TestCSAMergedRawZero(t *testing.T) {
	data := mutateCSAWorksheet(t, csaInlineMergedFixture(t, ""), func(s string) string {
		return strings.Replace(s, `<c r="C4" t="inlineStr"><is><t></t></is></c>`, `<c r="C4"><v>0</v></c>`, 1)
	})
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted zero inside merge")
	}
}

func TestCSARawWorkbookGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		part   string
		change func(string) string
	}{
		{"overlapping preamble", "xl/worksheets/sheet1.xml", func(s string) string {
			return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="A1:C1"/><mergeCell ref="B1:D1"/></mergeCells>`, 1)
		}},
		{"out of order", "xl/worksheets/sheet1.xml", func(s string) string {
			s = strings.Replace(s, `r="B5"`, `r="PLACEHOLDER"`, 1)
			s = strings.Replace(s, `r="C5"`, `r="B5"`, 1)
			return strings.Replace(s, `r="PLACEHOLDER"`, `r="C5"`, 1)
		}},
		{"far column", "xl/worksheets/sheet1.xml", func(s string) string {
			return strings.Replace(s, `r="F5"`, `r="XFD5"`, 1)
		}},
		{"case collision", "xl/workbook.xml", func(s string) string {
			return strings.Replace(s, `name="CCM"`, `name="ccm"`, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateCSAPart(t, csaFixture(t, func(f *excelize.File) {
				if tc.name == "case collision" {
					_, _ = f.NewSheet("Other")
				}
			}), tc.part, tc.change)
			if tc.name == "case collision" {
				data = mutateCSAPart(t, data, tc.part, func(s string) string { return strings.Replace(s, `name="Other"`, `name="CCM"`, 1) })
			}
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted invalid workbook")
			}
		})
	}
}

func TestCSARawNumberFormat(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) {
		format := ";;;"
		style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &format})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellStyle("CCM", "H5", "H5", style); err != nil {
			t.Fatal(err)
		}
	})
	expected := 2
	result, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Nodes[1].Body, "Models / Column 08: 0") {
		t.Fatal("hidden zero lost")
	}
}

func TestCSAMergeRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  string
		want bool
	}{
		{"preamble", "B1:W1", true},
		{"domain", "A4:E4", true},
		{"domain short", "A4:D4", false},
		{"domain long", "A4:F4", false},
		{"control", "A5:E5", false},
		{"blank row", "A6:E6", false},
		{"trailer unpinned", "A228:E228", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateCSAWorksheet(t, csaFixture(t, nil), func(s string) string {
				return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="`+tc.ref+`"/></mergeCells>`, 1)
			})
			expected := 2
			_, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
			if (err == nil) != tc.want {
				t.Fatalf("success = %v, want %v: %v", err == nil, tc.want, err)
			}
		})
	}
}

func TestCSAPinnedTrailerMerges(t *testing.T) {
	data := mutateCSAWorksheet(t, csaFixture(t, func(f *excelize.File) {
		_ = f.SetCellValue("CCM", "A228", "End of Standard")
		_ = f.SetCellValue("CCM", "A229", "invented copyright")
	}), func(s string) string {
		return strings.Replace(s, `</mergeCells>`, `<mergeCell ref="A228:E228"/><mergeCell ref="A229:D229"/></mergeCells>`, 1)
	})
	if _, err := parseCSAPinned(data, fmt.Sprintf("%x", sha256.Sum256(data)), 228); err != nil {
		t.Fatalf("pinned trailer merges rejected: %v", err)
	}
	if _, err := parseCSA(data); err == nil {
		t.Fatal("accepted unpinned trailer merges")
	}
}

func TestCSADomainToken(t *testing.T) {
	for _, tc := range []struct{ name, id string }{{"InventedAbC", "C-01"}, {"Example (EX", "EX-01"}, {"Example EX)", "EX-01"}} {
		t.Run(tc.name, func(t *testing.T) {
			data := csaFixture(t, func(f *excelize.File) {
				_ = f.SetCellValue("CCM", "A4", tc.name)
				_ = f.SetCellValue("CCM", "C5", tc.id)
			})
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted invalid domain")
			}
		})
	}
}

func TestCSAHeaderBeyondW(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "X2", "unexpected") })
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted header beyond W")
	}
}

func TestCSAPinnedTrailerUnrestricted(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) {
		_ = f.SetCellValue("CCM", "B6", "trailer")
		_ = f.SetCellValue("CCM", "X7", "more")
	})
	if _, err := csaFixtureWithFooter(data); err != nil {
		t.Fatal(fmt.Errorf("pinned trailer: %w", err))
	}
	if _, err := parseCSA(data); err == nil {
		t.Fatal("accepted unpinned trailer")
	}
}

func TestOtherWorkbookRowsIgnoreMergeMetadata(t *testing.T) {
	data := mutateCSAWorksheet(t, csaFixture(t, nil), func(s string) string { return strings.Replace(s, `ref="F2:H2"`, `ref="invalid"`, 1) })
	if _, err := rows(data, "CCM", false); err != nil {
		t.Fatalf("merge metadata affected generic reader: %v", err)
	}
}

func TestCSABlankSharedStringBeyondHeader(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "X2", "unique invented empty marker") })
	data = mutateCSAPart(t, data, "xl/sharedStrings.xml", func(s string) string { return strings.Replace(s, "unique invented empty marker", "", 1) })
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err != nil {
		t.Fatalf("blank shared cell rejected: %v", err)
	}
}

func TestCSARejectsNoncanonicalCellReferences(t *testing.T) {
	for _, ref := range []string{"x5", "X05"} {
		t.Run(ref, func(t *testing.T) {
			data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "X5", "invented unexpected value") })
			data = mutateCSAWorksheet(t, data, func(s string) string { return strings.Replace(s, `r="X5"`, `r="`+ref+`"`, 1) })
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted noncanonical cell reference")
			}
		})
	}
}
