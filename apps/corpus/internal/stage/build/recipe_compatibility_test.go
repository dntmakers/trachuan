package build

import (
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/internal/retrieval"
)

func TestBuildRecipe_Schema8HistoricalGoldens(t *testing.T) {
	snapshot := stage.Snapshot{SourceList: []byte(`{}`)}
	for _, tc := range []struct {
		policy retrieval.Recipe
		want   string
	}{
		{retrieval.Recipe2(), "ce3ec5d987260fcdfe6308a00cb5193658fafec08084416d4313a2a3c46c7c8c"},
		{retrieval.Recipe3(), "4c5a1b68b03e133c29dda4a6d6895e562aee8f8b15de97e0668897cd9db6fecf"},
	} {
		if got := New(Config{}).recipeForSchema(snapshot, tc.policy, 8); got != tc.want {
			t.Fatalf("version %d: got %s want %s", tc.policy.Version, got, tc.want)
		}
	}
}

func TestBuildRecipe_CurrentSchemaRecipe3Golden(t *testing.T) {
	snapshot := stage.Snapshot{SourceList: []byte(`{}`)}
	got := New(Config{}).recipeForRetrieval(snapshot, retrieval.Recipe3())
	if want := "186125b6cebded72897d41bd5b6978f62b709cd67a18904534e3c83266126311"; got != want {
		t.Fatalf("recipe 3 build hash: got %s want %s", got, want)
	}
}

func TestBuildRecipe_SupportsRetrievalRecipe2(t *testing.T) {
	s := New(Config{})
	snapshot := stage.Snapshot{SourceList: []byte(`{}`)}
	old := s.recipeForRetrieval(snapshot, retrieval.Recipe2())
	if old != "5e37f78f3c9498b6c36a53cf9bfdd5b1d33beeca9f99f322691dc9fa101a7c7d" {
		t.Fatalf("recipe 2 build hash %s", old)
	}
	if !s.acceptsRecipeHash(snapshot, old) || !s.acceptsRecipeHash(snapshot, s.Recipe(snapshot)) || s.acceptsRecipeHash(snapshot, "other") {
		t.Fatal("build acceptance")
	}
}

func TestVerifyBuildArtifact_RejectsUnsupportedRetrievalRecipe(t *testing.T) {
	paired2 := retrieval.Recipe2()
	paired2.SiblingMerge = "article-clauses/1"
	paired3 := retrieval.Recipe3()
	paired3.SiblingMerge = ""
	changed := retrieval.Recipe3()
	changed.DocumentCap++
	for _, tc := range []struct {
		name   string
		recipe retrieval.Recipe
	}{{"recipe 2 with merge", paired2}, {"recipe 3 without merge", paired3}, {"changed value", changed}} {
		t.Run(tc.name, func(t *testing.T) {
			binding := "current-fixture"
			artifacts, art, run, cfg, _ := bindingArtifact(t, &binding, false, bindingOptions{recipe2: tc.recipe.Version == 2, recipe: &tc.recipe})
			if _, _, err := verifyBuildArtifact(artifacts, art, run, cfg); err == nil {
				t.Fatal("accepted unsupported retrieval recipe")
			}
		})
	}
}
