package command

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestEntryPointsUseSelectedSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("FRAMEWORKS_SOURCE_DIR", dir)
	if err := os.Mkdir("sources", 0700); err != nil {
		t.Fatal(err)
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "CCM"); err != nil {
		t.Fatal(err)
	}
	for _, group := range [][3]string{{"F2", "H2", "Models"}, {"I2", "N2", "Architecture"}, {"O2", "W2", "Organization"}} {
		if err := f.SetCellValue("CCM", group[0], group[2]); err != nil {
			t.Fatal(err)
		}
		if err := f.MergeCell("CCM", group[0], group[1]); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 23; i++ {
		address, err := excelize.CoordinatesToCellName(i, 3)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellValue("CCM", address, fmt.Sprintf("Column %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetCellValue("CCM", "A4", "Invented (EX)"); err != nil {
		t.Fatal(err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if err := os.WriteFile("chosen.xlsx", data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	reg := fmt.Sprintf(`frameworks:
  - code: alpha
    version: '1.0'
    name: Invented
    publisher: Invented
    language: en
    access: licensed
    ordinal: 1
    citation_scheme: invented
    parser: csa-ccm-4-1-xlsx
    parse_source: chosen.xlsx
    expected_controls: 1
    files:
      - path: missing.xlsx
        sha256: %s
        size: 1
        role: catalog
        format: xlsx
      - path: chosen.xlsx
        sha256: %x
        size: %d
        role: catalog
        format: xlsx
`, strings.Repeat("a", 64), sum, len(data))
	if err := os.WriteFile(filepath.Join("sources", "frameworks.yaml"), []byte(reg), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"alpha", "1.0"}, {"edges"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(args, &out); err != nil {
				t.Fatalf("selected source failed: %v", err)
			}
			want := ""
			if len(args) == 2 {
				want = "controls: 1\n"
			}
			if out.String() != want {
				t.Fatalf("output = %q, want %q", out.String(), want)
			}
		})
	}
}
