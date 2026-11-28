package cli

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/frameworks/build"
	"github.com/stretchr/testify/require"
)

func TestFrameworkEmbedder_ReadsVerifiedVectorRows(t *testing.T) {
	startFrameworkEmbedder(t)
	ctx := context.Background()
	service := newFrameworkEmbedder()
	chunks := []build.Chunk{{Ordinal: 0, Content: "Invented body", ContextPrefix: "Invented framework"}, {Ordinal: 1, Content: "Invented second body"}}
	hash := strings.Repeat("a", 64)
	result, err := service.Build(ctx, "fw:invented:v1", chunks, hash)
	require.NoError(t, err)
	artifact := build.EmbedArtifact{Index: result.Index, Vectors: result.Vectors}
	rows, err := service.Read(ctx, artifact, "fw:invented:v1", chunks, hash)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for i, row := range rows {
		require.Equal(t, i, row.Ordinal)
		require.Equal(t, 2, row.TokenCount)
		require.Len(t, row.Values, embedding.DocumentRecipe1().Dimensions)
		require.Equal(t, float32(1), row.Values[0])
	}
	_, err = service.Read(ctx, artifact, "fw:invented:v2", chunks, hash)
	require.ErrorContains(t, err, "identity mismatch")
	artifact.Vectors = []byte{0}
	_, err = service.Read(ctx, artifact, "fw:invented:v1", chunks, hash)
	require.Error(t, err)
}
