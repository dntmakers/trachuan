package build

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestClassificationIdentity_PreservesLegacyAndBindsSelection(t *testing.T) {
	s := New(Config{})
	run := stage.Run{Country: "vn", Rel: "20990101", Snapshot: stage.Snapshot{SourceList: []byte(`{}`)}}
	inv := Inventory{Version: 2, Country: "vn"}
	input := s.inputHash(run.Country, run.Rel, inv, correctionInput{})
	recipe := s.Recipe(run.Snapshot)
	if got := s.frozenInputHash(run, inv, correctionInput{}); got != input {
		t.Fatalf("no-set input=%q want %q", got, input)
	}
	for _, letter := range []string{"a", "b"} {
		run.Snapshot.ClassificationSetSHA256 = strings.Repeat(letter, 64)
		if got := s.frozenInputHash(run, inv, correctionInput{}); got != stage.RecipeHash("build-classification-set", "1", input, run.Snapshot.ClassificationSetSHA256) {
			t.Fatalf("input identity does not bind %s: %s", letter, got)
		}
		if s.Recipe(run.Snapshot) == recipe || s.acceptsRecipeHash(run.Snapshot, recipe) {
			t.Fatal("opted-in build accepts an older worker recipe")
		}
	}
}
