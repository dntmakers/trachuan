package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRecipe_BindsPDFExtractor(t *testing.T) {
	current := stepRecipe("parse")
	require.Equal(t, "26.11.0", current.Parameters["pdf_extractor_version"])
	require.Equal(t, []string{"-layout", "-enc", "UTF-8", "-eol", "unix", "-", "-"}, current.Parameters["pdf_layout_flags"])
	require.Equal(t, []string{"-bbox-layout", "-enc", "UTF-8", "-eol", "unix", "-", "-"}, current.Parameters["pdf_bbox_flags"])
	for _, key := range []string{"pdf_extractor_version", "pdf_layout_flags", "pdf_bbox_flags"} {
		t.Run(key, func(t *testing.T) {
			base := t.TempDir()
			tmp, target := filepath.Join(base, "tmp"), filepath.Join(base, "parse")
			require.NoError(t, os.Mkdir(tmp, 0700))
			require.NoError(t, writeFile(tmp, "artifact", []byte("invented")))
			previous := stepRecipe("parse")
			previous.Parameters[key] = "previous"
			require.NoError(t, seal(tmp, target, previous, nil))
			require.ErrorContains(t, validate(target, stepRecipe("parse"), nil), "inputs or recipe changed")
		})
	}
}
