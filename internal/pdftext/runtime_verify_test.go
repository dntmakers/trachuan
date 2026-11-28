package pdftext

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeIdentity_BindsBuildAndCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, []byte("invented"), 0600); err != nil {
		t.Fatal(err)
	}
	base := runtimeSet{files: []string{path}, platform: platformIdentity{build: "build-a", caches: []cacheIdentity{{path: "/cache/a", uuid: [16]byte{1}}}}}
	first, err := runtimeIdentity(base, "version")
	if err != nil {
		t.Fatal(err)
	}
	other := base
	other.platform.build = "build-b"
	second, err := runtimeIdentity(other, "version")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("OS build not bound")
	}
	other = base
	other.platform.caches = []cacheIdentity{{path: "/cache/a", uuid: [16]byte{2}}}
	third, err := runtimeIdentity(other, "version")
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("cache UUID not bound")
	}
}

func TestRuntimeSnapshot_DetectsStatFields(t *testing.T) {
	for _, change := range []string{"mode", "mtime", "inode"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			data := filepath.Join(root, "data")
			if err := os.Mkdir(data, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(data, "cmap")
			if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
				t.Fatal(err)
			}
			set := runtimeSet{dataDirs: []string{data}}
			snapshot, err := snapshotRuntime(set)
			if err != nil {
				t.Fatal(err)
			}
			fileInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			dirInfo, err := os.Stat(data)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "mode":
				err = os.Chmod(path, 0644)
			case "mtime":
				err = os.Chtimes(path, fileInfo.ModTime().Add(time.Second), fileInfo.ModTime().Add(time.Second))
			case "inode":
				replacement := filepath.Join(root, "replacement")
				if err = os.WriteFile(replacement, []byte("first"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Chtimes(replacement, fileInfo.ModTime(), fileInfo.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
				err = os.Chtimes(data, dirInfo.ModTime(), dirInfo.ModTime())
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := checkRuntimeSnapshot(set, snapshot); err == nil {
				t.Fatalf("%s change accepted", change)
			}
		})
	}
}

func TestRuntimeSnapshot_DetectsChangedAndAddedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmap")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	set := runtimeSet{dataDirs: []string{root}}
	snapshot, err := snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	if checkErr := checkRuntimeSnapshot(set, snapshot); checkErr != nil {
		t.Fatal(checkErr)
	}
	if writeErr := os.WriteFile(path, []byte("changed"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if checkErr := checkRuntimeSnapshot(set, snapshot); checkErr == nil {
		t.Fatal("changed file accepted")
	}
	snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkRuntimeSnapshot(set, snapshot); err == nil {
		t.Fatal("new file accepted")
	}
}
