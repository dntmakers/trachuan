package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestParseVersionUsesSelectedSource(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	require.NoError(t, f.SetSheetName("Sheet1", "CCM"))
	for _, group := range [][3]string{{"F2", "H2", "Models"}, {"I2", "N2", "Architecture"}, {"O2", "W2", "Organization"}} {
		require.NoError(t, f.SetCellValue("CCM", group[0], group[2]))
		require.NoError(t, f.MergeCell("CCM", group[0], group[1]))
	}
	for i := 1; i <= 23; i++ {
		address, err := excelize.CoordinatesToCellName(i, 3)
		require.NoError(t, err)
		require.NoError(t, f.SetCellValue("CCM", address, fmt.Sprintf("Column %d", i)))
	}
	require.NoError(t, f.SetCellValue("CCM", "A4", "Chosen Domain (CH)"))
	require.NoError(t, f.SetCellValue("CCM", "B5", "Chosen title"))
	require.NoError(t, f.SetCellValue("CCM", "C5", "CH-01"))
	require.NoError(t, f.SetCellValue("CCM", "D5", "Chosen statement"))
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files", "chosen.xlsx"), buf.Bytes(), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files", "other.xlsx"), []byte("not selected"), 0600))
	count := 2
	v := registry.Framework{Parser: "csa-ccm-4-1-xlsx", ParseSource: "chosen.xlsx", ExpectedControls: &count, Files: []registry.File{{Path: "other.xlsx", Role: "catalog", Format: "xlsx"}, {Path: "chosen.xlsx", Role: "catalog", Format: "xlsx", Size: int64(buf.Len()), SHA256: digest(buf.Bytes())}}}
	output := filepath.Join(dir, "parse")
	require.NoError(t, os.Mkdir(output, 0700))
	frozen := &snapshotParseInputs{registry: &registry.Registry{Frameworks: []registry.Framework{v}}, sources: map[string][]byte{"chosen.xlsx": buf.Bytes(), "other.xlsx": []byte("not selected")}}
	require.NoError(t, parseStepWith(context.Background(), dir, output, nil, nil, frozen))
	catalogs := readRows[catalog](t, filepath.Join(output, "catalogs.jsonl"))
	require.Len(t, catalogs, 1)
	require.Len(t, catalogs[0].Nodes, 2)
	require.Equal(t, "CH-01", catalogs[0].Nodes[1].Citation)
}
