package eval

import (
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
)

func TestBindBuildWarnings_ReadsArtifactPair(t *testing.T) {
	dir := t.TempDir()
	cases := []evaluation.WarningCase{{EntryID: "effect-example"}}
	result, err := bindBuildWarnings(dir, "vn", cases)
	if err != nil || len(result) != 1 || result[0].Outcome != "missing" {
		t.Fatalf("missing files: %+v, %v", result, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "knowledge.yaml"), []byte("[]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = bindBuildWarnings(dir, "vn", cases); err == nil {
		t.Fatal("unpaired files accepted")
	}
}
