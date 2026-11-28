package parse

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func csaFixture(t *testing.T, mutate func(*excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "CCM"); err != nil {
		t.Fatal(err)
	}
	for _, m := range [][3]string{{"F2", "H2", "Models"}, {"I2", "N2", "Architecture"}, {"O2", "W2", "Organization"}} {
		if err := f.SetCellValue("CCM", m[0], m[2]); err != nil {
			t.Fatal(err)
		}
		if err := f.MergeCell("CCM", m[0], m[1]); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 23; i++ {
		address, _ := excelize.CoordinatesToCellName(i, 3)
		if err := f.SetCellValue("CCM", address, fmt.Sprintf("Column %02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for address, value := range map[string]any{"A4": "Example Domain (EX)", "B5": "Invented title", "C5": "EX-01", "D5": "Invented\nstatement", "E5": false, "F5": "yes", "H5": 0} {
		if err := f.SetCellValue("CCM", address, value); err != nil {
			t.Fatal(err)
		}
	}
	if mutate != nil {
		mutate(f)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCSAParseInventedWorkbook(t *testing.T) {
	expected := 2
	result, err := Parse("csa-ccm-4-1-xlsx", csaFixture(t, nil), "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Nodes[0].Citation != "EX" || result.Nodes[0].Parent != -1 || result.Nodes[1].Citation != "EX-01" || result.Nodes[1].Parent != 0 {
		t.Fatal("incorrect hierarchy")
	}
	var want strings.Builder
	want.WriteString("Invented statement\n\nColumn 05: FALSE\nModels / Column 06: yes\nModels / Column 07:\nModels / Column 08: 0")
	for i := 9; i <= 23; i++ {
		group := "Architecture"
		if i >= 15 {
			group = "Organization"
		}
		fmt.Fprintf(&want, "\n%s / Column %02d:", group, i)
	}
	if result.Nodes[1].Body != want.String() {
		t.Fatalf("body = %q, want %q", result.Nodes[1].Body, want.String())
	}
}

func TestCSAPreservesInternalWhitespace(t *testing.T) {
	expected := 2
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "D5", "  First  second\nthird  ") })
	result, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Nodes[1].Body, "First  second third\n\n") {
		t.Fatal("internal spaces changed")
	}
}

func TestCSARejectsNonpositiveCount(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) {
		for _, cell := range []string{"A4", "B5", "C5", "D5", "E5", "F5", "H5"} {
			_ = f.SetCellValue("CCM", cell, "")
		}
	})
	expected := 0
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted zero count")
	}
}

func TestCSADeterministicAndIgnoresOtherSheets(t *testing.T) {
	expected := 2
	data := csaFixture(t, func(f *excelize.File) {
		_, _ = f.NewSheet("Other")
		_ = f.SetCellValue("Other", "A1", "invented sentinel")
	})
	first, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("non-deterministic result")
	}
}

func TestCSAPinnedFooter(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "A6", "invented footer") })
	if _, err := csaFixtureWithFooter(data); err != nil {
		t.Fatal("pinned invented footer rejected")
	}
	if _, err := parseCSA(data); err == nil {
		t.Fatal("unknown digest accepted")
	}
	original := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, tc := range []struct {
		name   string
		mutate func(*excelize.File)
	}{
		{"changed row", func(f *excelize.File) { _ = f.SetCellValue("CCM", "A6", "changed footer") }},
		{"shifted row", func(f *excelize.File) {
			_ = f.SetCellValue("CCM", "A6", "")
			_ = f.SetCellValue("CCM", "A7", "invented footer")
		}},
		{"extra column", func(f *excelize.File) { _ = f.SetCellValue("CCM", "B6", "extra") }},
		{"extra suffix", func(f *excelize.File) { _ = f.SetCellValue("CCM", "A7", "unexpected") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "A6", "invented footer"); tc.mutate(f) })
			if _, err := parseCSAPinned(changed, original, 6); err == nil {
				t.Fatal("changed workbook accepted with old digest")
			}
			if _, err := csaFixtureWithFooter(changed); err != nil {
				t.Fatal("pinned trailer rejected")
			}
		})
	}
}

func csaFixtureWithFooter(data []byte) ([]Node, error) {
	return parseCSAPinned(data, fmt.Sprintf("%x", sha256.Sum256(data)), 6)
}

func TestCSAGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*excelize.File)
		count  int
	}{
		{"count", nil, 3},
		{"shifted merge", func(f *excelize.File) { _ = f.UnmergeCell("CCM", "F2", "H2"); _ = f.MergeCell("CCM", "E2", "H2") }, 2},
		{"missing header", func(f *excelize.File) { _ = f.SetCellValue("CCM", "J3", "") }, 2},
		{"unknown header", func(f *excelize.File) { _ = f.SetCellValue("CCM", "X3", "Unknown") }, 2},
		{"wrong sheet", func(f *excelize.File) { _ = f.SetSheetName("CCM", "wrong") }, 2},
		{"missing parent", func(f *excelize.File) { _ = f.SetCellValue("CCM", "A4", "") }, 2},
		{"missing title", func(f *excelize.File) { _ = f.SetCellValue("CCM", "B5", "") }, 2},
		{"duplicate header", func(f *excelize.File) { _ = f.SetCellValue("CCM", "J3", "Column 09") }, 2},
		{"wrong parent", func(f *excelize.File) { _ = f.SetCellValue("CCM", "C5", "ZZ-01") }, 2},
		{"malformed id", func(f *excelize.File) { _ = f.SetCellValue("CCM", "C5", "EX-1") }, 2},
		{"missing statement", func(f *excelize.File) { _ = f.SetCellValue("CCM", "D5", "") }, 2},
		{"unexpected applicability row", func(f *excelize.File) { _ = f.SetCellValue("CCM", "F6", "unexpected") }, 2},
		{"duplicate citation", func(f *excelize.File) {
			_ = f.SetCellValue("CCM", "B6", "Another")
			_ = f.SetCellValue("CCM", "C6", "EX-01")
			_ = f.SetCellValue("CCM", "D6", "Statement")
		}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("csa-ccm-4-1-xlsx", csaFixture(t, tc.mutate), "invented", "1", &tc.count)
			if err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

func TestCSARejectsMissingGroupMerges(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) {
		if err := f.UnmergeCell("CCM", "F2", "H2"); err != nil {
			t.Fatal(err)
		}
	})
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted missing group merges")
	}
}

func TestCSARejectsOtherRowTwoLabels(t *testing.T) {
	for _, address := range []string{"A2", "E2", "G2", "J2", "W2"} {
		t.Run(address, func(t *testing.T) {
			data := csaFixture(t, func(f *excelize.File) {
				_ = f.UnmergeCell("CCM", "F2", "H2")
				_ = f.UnmergeCell("CCM", "I2", "N2")
				_ = f.UnmergeCell("CCM", "O2", "W2")
				_ = f.SetCellValue("CCM", address, "unexpected")
			})
			data = mutateCSAWorksheet(t, data, func(s string) string {
				return strings.Replace(s, `</worksheet>`, `<mergeCells count="3"><mergeCell ref="F2:H2"/><mergeCell ref="I2:N2"/><mergeCell ref="O2:W2"/></mergeCells></worksheet>`, 1)
			})
			expected := 2
			wantError := "invalid workbook merge"
			if address == "A2" || address == "E2" {
				wantError = "invalid workbook header"
			}
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil || err.Error() != wantError {
				t.Fatalf("expected %s, got %v", wantError, err)
			}
		})
	}
}

func TestCSARejectsEmptyGroupAnchor(t *testing.T) {
	for _, address := range []string{"F2", "I2", "O2"} {
		t.Run(address, func(t *testing.T) {
			data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", address, "") })
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil || err.Error() != "invalid workbook header" {
				t.Fatalf("expected invalid workbook header, got %v", err)
			}
		})
	}
}

func TestCSATrailerAt228(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) {
		_ = f.SetCellValue("CCM", "B227", "Last control")
		_ = f.SetCellValue("CCM", "C227", "EX-02")
		_ = f.SetCellValue("CCM", "D227", "Last statement")
		_ = f.SetCellValue("CCM", "A228", "End of Standard")
		_ = f.SetCellValue("CCM", "A229", "invented copyright")
	})
	nodes, err := parseCSAPinned(data, fmt.Sprintf("%x", sha256.Sum256(data)), 228)
	if err != nil {
		t.Fatalf("pinned trailer rejected: %v", err)
	}
	if len(nodes) != 3 || nodes[2].Citation != "EX-02" || nodes[2].PublisherTitle != "Last control" {
		t.Fatalf("row 227 control missing: %+v", nodes)
	}
	if _, err := parseCSA(data); err == nil {
		t.Fatal("accepted unpinned trailer")
	}
}

func TestCSARejectsUnexpectedColumnInTable(t *testing.T) {
	for _, address := range []string{"X4", "X5", "X6"} {
		t.Run(address, func(t *testing.T) {
			data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", address, "unexpected") })
			expected := 2
			if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
				t.Fatal("accepted unexpected table column")
			}
		})
	}
}

func TestCSARejectsApplicabilityOnDomainRow(t *testing.T) {
	data := csaFixture(t, func(f *excelize.File) { _ = f.SetCellValue("CCM", "F4", "unexpected") })
	expected := 2
	if _, err := Parse("csa-ccm-4-1-xlsx", data, "invented", "1", &expected); err == nil {
		t.Fatal("accepted applicability on domain row")
	}
}
