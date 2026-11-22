package parse

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
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
