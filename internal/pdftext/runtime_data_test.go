//go:build !windows

package pdftext

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestRuntimeIdentity_ExcludesCacheFileAliases(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if fileErr := os.Mkdir(cache, 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	file := filepath.Join(cache, "library")
	if fileErr := os.WriteFile(file, []byte("first"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	alias := filepath.Join(root, "library")
	if fileErr := os.Symlink(file, alias); fileErr != nil {
		t.Fatal(fileErr)
	}
	set := runtimeSet{systemFiles: []string{alias}, systemDirs: []string{cache}, cacheDirs: []string{cache}}
	first, err := runtimeIdentity(set, "version")
	if err != nil {
		t.Fatal(err)
	}
	if fileErr := os.WriteFile(file, []byte("other"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	second, err := runtimeIdentity(set, "version")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("dyld cache alias changed identity")
	}
}

func TestRuntimeIdentity_FollowsDataFileLinks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first", "second"} {
		if fileErr := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); fileErr != nil {
			t.Fatal(fileErr)
		}
	}
	link := filepath.Join(root, "selected")
	if fileErr := os.Symlink("first", link); fileErr != nil {
		t.Fatal(fileErr)
	}
	set := runtimeSet{dataDirs: []string{root}}
	first, err := runtimeIdentity(set, "version")
	if err != nil {
		t.Fatal(err)
	}
	if fileErr := os.Remove(link); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.Symlink("second", link); fileErr != nil {
		t.Fatal(fileErr)
	}
	second, err := runtimeIdentity(set, "version")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("data symlink changed without changing identity")
	}
}

func TestRuntimeDataPaths_FollowsFileSymlinks(t *testing.T) {
	cellar, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	poppler := filepath.Join(cellar, "poppler", "26.11.0")
	data := filepath.Join(cellar, "poppler-data", "1.2.3", "share", "maps")
	if fileErr := os.MkdirAll(filepath.Join(poppler, "share", "poppler"), 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.MkdirAll(data, 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	target := filepath.Join(data, "actual")
	if fileErr := os.WriteFile(target, []byte("invented"), 0600); fileErr != nil {
		t.Fatal(fileErr)
	}
	middle := filepath.Join(data, "middle")
	if fileErr := os.Symlink("actual", middle); fileErr != nil {
		t.Fatal(fileErr)
	}
	alias := filepath.Join(poppler, "share", "poppler", "cmap")
	if fileErr := os.Symlink(middle, alias); fileErr != nil {
		t.Fatal(fileErr)
	}
	set, err := runtimeDataPaths(cellar, poppler)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range set.files {
		if path == middle {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing intermediate data link: %v", set.files)
	}
}

func TestRuntimeDataKegs_FollowsSeparateKeg(t *testing.T) {
	cellar := t.TempDir()
	cellar, err := filepath.EvalSymlinks(cellar)
	if err != nil {
		t.Fatal(err)
	}
	poppler := filepath.Join(cellar, "poppler", "26.11.0")
	data := filepath.Join(cellar, "poppler-data", "1.2.3", "share", "poppler")
	if err = os.MkdirAll(filepath.Join(poppler, "share"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(data, filepath.Join(poppler, "share", "poppler")); err != nil {
		t.Fatal(err)
	}
	got, err := runtimeDataPaths(cellar, poppler)
	want := []string{data, filepath.Join(poppler, "share", "poppler")}
	sort.Strings(want)
	if err != nil || !reflect.DeepEqual(got.dataDirs, want) {
		t.Fatalf("data paths: %v, %v", got.dataDirs, err)
	}
}

func TestRuntimeDataKegs_RejectsOutsideKeg(t *testing.T) {
	cellar := t.TempDir()
	outside := t.TempDir()
	poppler := filepath.Join(cellar, "poppler", "26.11.0")
	if fileErr := os.MkdirAll(filepath.Join(poppler, "share"), 0700); fileErr != nil {
		t.Fatal(fileErr)
	}
	if fileErr := os.Symlink(outside, filepath.Join(poppler, "share", "poppler")); fileErr != nil {
		t.Fatal(fileErr)
	}
	if _, err := runtimeDataPaths(cellar, poppler); err == nil {
		t.Fatal("outside data accepted")
	}
}
