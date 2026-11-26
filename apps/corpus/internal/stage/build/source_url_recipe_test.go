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
	if got != "6629d13cd49ef3a4a1a05b2d393862be40e6e7142ee91ede114f360483e6bd5f" {
		t.Fatalf("build recipe = %s", got)
	}
}

func TestBuildRecipe_HeadingEvidenceInvalidatesCachedBuild(t *testing.T) {
	if got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)}); got == "620f4d434ff1822173d4467ad6714fc492d9012aae899336208c6f24379f306d" {
		t.Fatal("heading evidence reused the build recipe without a stored projection")
	}
}
