package build

import (
	"encoding/json"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/internal/retrieval"
)

func TestRecipe4_RetainedRecipe3Artifact(t *testing.T) {
	binding := "current-fixture"
	policy := retrieval.Recipe3()
	cfg := Config{Suite: evaluation.Suite{Hashes: evaluation.FixtureHashes{IdentityMap: "map", Development: binding}}}
	snapshot := stage.Snapshot{SourceList: []byte(`{"country":"vn","status_codes":{}}`)}
	hash := New(cfg).recipeForRetrieval(snapshot, policy)
	artifacts, art, run, config, _ := bindingArtifact(t, &binding, false, bindingOptions{recipe: &policy, buildRecipeHash: hash})
	if _, _, err := verifyBuildArtifact(artifacts, art, run, config); err != nil {
		t.Fatal(err)
	}
}

func TestRecipe4_NewAndRetainedBuilds(t *testing.T) {
	s := New(Config{})
	snapshot := stage.Snapshot{SourceList: []byte(`{}`)}
	if got, want := s.Recipe(snapshot), s.recipeForRetrieval(snapshot, retrieval.Recipe4()); got != want {
		t.Fatalf("new build recipe = %s, want %s", got, want)
	}
	for _, recipe := range []retrieval.Recipe{retrieval.Recipe2(), retrieval.Recipe3(), retrieval.Recipe4()} {
		raw, err := json.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.recordRecipeHash(snapshot, raw, 9)
		if err != nil {
			t.Fatalf("recipe %d: %v", recipe.Version, err)
		}
		if want := s.recipeForSchema(snapshot, recipe, 9); got != want {
			t.Fatalf("recipe %d: got %s want %s", recipe.Version, got, want)
		}
	}
}
