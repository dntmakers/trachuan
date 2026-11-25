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
	if got != "3ee9e4ccba68c9479449c2e667e37164f2ebe69b05fc0c17add1d3aed5c967fe" {
		t.Fatalf("build recipe = %s", got)
	}
}

func TestBuildRecipe_HeadingEvidenceInvalidatesCachedBuild(t *testing.T) {
	if got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)}); got == "620f4d434ff1822173d4467ad6714fc492d9012aae899336208c6f24379f306d" {
		t.Fatal("heading evidence reused the build recipe without a stored projection")
	}
}
