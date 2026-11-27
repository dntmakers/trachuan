package build

import (
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestBuildRecipe_ProjectionVersion(t *testing.T) {
	got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)})
	if got == "32be8523e5288ab734afb45ed7d6ce3b0c45cdc51d78331438425bf11a10cb1a" {
		t.Fatal("projection did not change build recipe")
	}
	if got != "4486d99a1cd047cdb5919d264b7401eeb1d76fbdad4fac2cba77ba63d5f59759" {
		t.Fatalf("build recipe = %s", got)
	}
}

func TestBuildRecipe_HeadingEvidenceInvalidatesCachedBuild(t *testing.T) {
	if got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)}); got == "620f4d434ff1822173d4467ad6714fc492d9012aae899336208c6f24379f306d" {
		t.Fatal("heading evidence reused the build recipe without a stored projection")
	}
}
