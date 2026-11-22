package chunk

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestChunkLineageRequiresFrozenContract(t *testing.T) {
	dir := t.TempDir()
	fetchHash, recipe := strings.Repeat("a", 64), strings.Repeat("b", 64)
	legacy := stage.Snapshot{ExtractRecordContract: stage.ExtractRecordLegacy, Recipes: map[string]string{"extract": recipe}}
	if err := checkExtractLineageForSnapshot(dir, fetchHash, fetchHash, legacy, recipe); err != nil {
		t.Fatalf("legacy missing text: %v", err)
	}
	v1 := legacy
	v1.ExtractRecordContract = stage.ExtractRecordV1
	if err := checkExtractLineageForSnapshot(dir, fetchHash, fetchHash, v1, recipe); err == nil {
		t.Fatal("v1 accepted missing text")
	}
	if err := checkExtractLineageForSnapshot(dir, fetchHash, fetchHash, stage.Snapshot{}, recipe); err == nil {
		t.Fatal("accepted missing contract and recipe")
	}
	if err := checkExtractLineageForSnapshot(dir, fetchHash, fetchHash, legacy, strings.Repeat("c", 64)); err == nil {
		t.Fatal("accepted mismatched recipe")
	}
}
