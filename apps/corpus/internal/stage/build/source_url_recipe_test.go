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
	if got != "4c5a1b68b03e133c29dda4a6d6895e562aee8f8b15de97e0668897cd9db6fecf" {
		t.Fatalf("build recipe = %s", got)
	}
}

func TestBuildRecipe_HeadingEvidenceInvalidatesCachedBuild(t *testing.T) {
	if got := New(Config{}).Recipe(stage.Snapshot{SourceList: []byte(`{}`)}); got == "620f4d434ff1822173d4467ad6714fc492d9012aae899336208c6f24379f306d" {
		t.Fatal("heading evidence reused the build recipe without a stored projection")
	}
}
