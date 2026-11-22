package build

import (
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestBuild_WarningFixtureDoesNotChangeInputOrRecipe(t *testing.T) {
	suite := evaluation.Suite{WarningHash: "first", Hashes: evaluation.FixtureHashes{IdentityMap: "map", Development: "development"}}
	inv := Inventory{Country: "vn"}
	correction := correctionInput{hash: hashBytes([]byte("[]"))}
	snapshot := stage.Snapshot{SourceList: []byte(`{"country":"vn","status_codes":{}}`)}
	first := New(Config{Suite: suite})
	input, recipe := first.inputHash("vn", "20990101", inv, correction), first.Recipe(snapshot)
	suite.WarningHash = "second"
	second := New(Config{Suite: suite})
	if input != second.inputHash("vn", "20990101", inv, correction) || recipe == "" || recipe != second.Recipe(snapshot) {
		t.Fatal("warning fixture changed build input or serving recipe")
	}
}
