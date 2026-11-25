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
	if err != nil || hash != "85d69c51bfe41da43fc190c07bf5805ce6cea4cee3a089671f575b0c80c3af11" {
		t.Fatalf("schema SHA256=%s err=%v", hash, err)
	}

}
