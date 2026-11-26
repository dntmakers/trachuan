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
	if got != "1d854a474ac9b049e5755a71a3b56bdc230ea032be451b5227b75a5140133928" {
		t.Fatalf("build recipe = %s", got)
	}
}

func TestBuildRecipe_HeadingEvidenceInvalidatesCachedBuild(t *testing.T) {
	if got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)}); got == "620f4d434ff1822173d4467ad6714fc492d9012aae899336208c6f24379f306d" {
		t.Fatal("heading evidence reused the build recipe without a stored projection")
	}
}
