package corpusdb

import (
	"slices"
	"strings"
	"testing"
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
	if err != nil || hash != "d8c766d0d5c6a322b993eb3019450e583938cc1b1878b4ad1c5bc522e5f1305e" {
		t.Fatalf("schema SHA256=%s err=%v", hash, err)
	}

}
