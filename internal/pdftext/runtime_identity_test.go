package pdftext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeIdentity_SortsFilesAndBindsVersion(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if fileErr := os.WriteFile(first, []byte("one"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.WriteFile(second, []byte("two"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	a, err := runtimeIdentity(runtimeSet{files: []string{first, second}}, "version-a\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := runtimeIdentity(runtimeSet{files: []string{second, first}}, "version-a\n")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("file order changed identity")
	}
	c, err := runtimeIdentity(runtimeSet{files: []string{first, second}}, "version-b\n")
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("version change did not alter identity")
	}
	if fileErr := os.WriteFile(first, []byte("two"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	d, err := runtimeIdentity(runtimeSet{files: []string{first, second}}, "version-a\n")
	if err != nil || a == d {
		t.Fatalf("runtime file change did not alter identity: %v", err)
	}
}

func TestRuntimeIdentity_DataByteChangesHash(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "pdftotext")
	data := filepath.Join(root, "poppler-data")
	if fileErr := os.Mkdir(data, 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.WriteFile(exe, []byte("invented binary"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	file := filepath.Join(data, "cmap")
	if fileErr := os.WriteFile(file, []byte("mapping-a"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	set := runtimeSet{files: []string{exe}, dataDirs: []string{data}}
	first, err := runtimeIdentity(set, "pdftotext version 26.11.0\n")
	if err != nil {
		t.Fatal(err)
	}
	if fileErr := os.WriteFile(file, []byte("mapping-b"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	second, err := runtimeIdentity(set, "pdftotext version 26.11.0\n")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("data content change did not alter identity")
	}
}
