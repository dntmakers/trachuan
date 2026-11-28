package serve_test

import (
	"testing"

	"danny.vn/trachuan/internal/frameworks/build"
	"danny.vn/trachuan/internal/frameworks/serve"
	"github.com/stretchr/testify/require"
)

func TestCurrentBuildStepVersions_AreAcceptedForServing(t *testing.T) {
	versions := build.PublishedStepVersions()
	require.ElementsMatch(t, []string{"chunk", "snapshot", "parse", "relate", "embed"}, keys(versions))
	for step, version := range versions {
		t.Run(step, func(t *testing.T) {
			require.Truef(t, serve.SupportsStepVersion(step, version), "build version %d is not accepted", version)
		})
	}
}

func keys(versions map[string]int) []string {
	result := make([]string, 0, len(versions))
	for key := range versions {
		result = append(result, key)
	}
	return result
}
