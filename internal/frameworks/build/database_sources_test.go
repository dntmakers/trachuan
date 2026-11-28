package build

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type sourceFileCapture struct{ rows [][]any }

func (db *sourceFileCapture) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	db.rows = append(db.rows, append([]any(nil), args...))
	return pgconn.CommandTag{}, nil
}

func TestSourceProvenanceRecipe(t *testing.T) {
	require.Equal(t, "source-selection/2", databaseRecipe()["source_provenance"])
	require.Equal(t, "csa-ccm-4-1-xlsx/2", stepRecipe("parse").Parameters["csa_parser"])
}

func TestLoadSourceFiles_SelectedCompletenessAndRepresentation(t *testing.T) {
	v := registry.Framework{Code: "invented", Version: "1", Parser: "csa-ccm-4-1-xlsx", ParseSource: "selected.xlsx", Files: []registry.File{
		{Path: "selected.xlsx", Role: "catalog", Format: "xlsx"},
		{Path: "other.xlsx", Role: "catalog", Format: "xlsx"},
		{Path: "mapping.xlsx", Role: "mapping workbook", Format: "xlsx"},
		{Path: "other.pdf", Role: "text", Format: "pdf"},
	}}
	db := &sourceFileCapture{}
	err := loadSourceFiles(context.Background(), db, databaseInputs{versions: []registry.Framework{v}}, databaseIDs{versions: map[string]int64{"invented:1": 1}})
	require.NoError(t, err)
	require.Len(t, db.rows, 4)
	for _, row := range db.rows {
		require.Len(t, row, 9)
		name := row[2].(string)
		source := "structured"
		if name == "other.pdf" {
			source = "pdf_text"
		}
		require.Equal(t, source, row[7])
		require.Equal(t, name == "selected.xlsx", row[8])
	}
}

func TestLoadSourceFiles_OnlyConsumedRoles(t *testing.T) {
	d := databaseInputs{versions: []registry.Framework{{Code: "fw-alpha", Version: "v1", LicenceKind: "synthetic", Files: []registry.File{
		{Path: "e-change-log.pdf", Role: "change log"},
		{Path: "d-text.json", Role: "text"},
		{Path: "a-guide.pdf", Role: "guide"},
		{Path: "c-mapping.xlsx", Role: "mapping workbook"},
		{Path: "b-catalog.json", Role: "catalog"},
	}}}}
	for i := range d.versions[0].Files {
		file := &d.versions[0].Files[i]
		file.SHA256 = digest([]byte(file.Path))
		file.Size = int64(len(file.Path))
	}
	db := &sourceFileCapture{}
	require.NoError(t, loadSourceFiles(context.Background(), db, d, databaseIDs{versions: map[string]int64{"fw-alpha:v1": 1}}))
	require.Len(t, db.rows, 3)
	for i, want := range []struct{ name, role string }{{"b-catalog.json", "catalog"}, {"c-mapping.xlsx", "mapping"}, {"d-text.json", "text"}} {
		require.Equal(t, i+1, db.rows[i][0])
		require.Equal(t, want.name, db.rows[i][2])
		require.Equal(t, want.role, db.rows[i][3])
	}
	require.Equal(t, "change log", d.versions[0].Files[0].Role)
}
