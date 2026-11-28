//go:build !windows

package pdftext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePath_TracksSymlinkChain(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(root, "real")
	if fileErr := os.Mkdir(realDir, 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.WriteFile(filepath.Join(realDir, "lib.1.2"), []byte("x"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.Symlink("lib.1.2", filepath.Join(realDir, "lib.1")); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.Symlink("real", filepath.Join(root, "alias")); fileErr != nil {
		t.Fatal(fileErr)
	}
	resolved, links, directories, err := traceRuntimePath(filepath.Join(root, "alias", "lib.1"))
	if err != nil || resolved != filepath.Join(realDir, "lib.1.2") {
		t.Fatalf("resolution: %q, %v", resolved, err)
	}
	want := filepath.Join(realDir, "lib.1")
	found := false
	for _, link := range links {
		if link == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing intermediate link: %v", links)
	}
	if len(directories) != 1 || directories[0] != filepath.Join(root, "alias") {
		t.Fatalf("missing directory link: %v", directories)
	}
}
