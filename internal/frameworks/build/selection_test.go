package build

import (
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
	v := registry.Framework{Parser: "csa-ccm-4-1-xlsx", ParseSource: "chosen.xlsx", ExpectedControls: &count, Files: []registry.File{{Path: "other.xlsx", Role: "catalog", Format: "xlsx"}, {Path: "chosen.xlsx", Role: "catalog", Format: "xlsx"}}}
	result, err := parseVersion(dir, v)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 2)
	require.Equal(t, "CH-01", result.Nodes[1].Citation)
}
