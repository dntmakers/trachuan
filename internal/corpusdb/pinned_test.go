package corpusdb

import (
	"slices"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestSchema_PinsKeepVersion5Supported(t *testing.T) {
	if SchemaVersion != 9 || !slices.Equal(SupportedSchemaVersions(), []int{4, 5, 6, 7, 8, 9}) {
		t.Fatalf("schema=%d supported=%v", SchemaVersion, SupportedSchemaVersions())
	}
	for _, part := range []string{"schema_version = 9", "source_withdrawn_since date", "source_withdrawn_rel text"} {
		if !strings.Contains(schema, part) {
			t.Errorf("schema missing %s", part)
		}
	}
	hash, err := SchemaSHA256(1024)
	if err != nil || hash != "f719664266c01b01a4dad1c4fe16993fedf93463c47a66116c5fa72e754118f8" {
		t.Fatalf("schema SHA256=%s err=%v", hash, err)
	}

}

func TestSchema_ReleaseChangeSetColumnsEqualKinds(t *testing.T) {
	start := strings.Index(schema, "CREATE TABLE release_change_set")
	table := schema[start : start+strings.Index(schema[start:], ");")]
	var columns []string
	for _, line := range strings.Split(table, "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		columns = append(columns, name)
	}
	from, to := slices.Index(columns, "baseline"), slices.Index(columns, "digest")
	if from < 0 || to < from || !slices.Equal(columns[from+1:to], releasechanges.Kinds()) {
		t.Fatalf("release_change_set count columns %v, kinds %v", columns, releasechanges.Kinds())
	}
	if releasechanges.CountColumns() != strings.Join(columns[from+1:to], ",") {
		t.Fatal("count column list differs from schema")
	}
}
