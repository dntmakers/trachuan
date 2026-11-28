package pdftext

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type runtimeEntry struct {
	path   string
	link   os.FileInfo
	target os.FileInfo
	hash   bool
}

type runtimeSnapshot []runtimeEntry

func cacheContains(roots []string, path string) (bool, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		base, err := filepath.EvalSymlinks(root)
		if err != nil {
			return false, err
		}
		rel, err := filepath.Rel(base, real)
		if err != nil {
			return false, err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}

func snapshotRuntime(set runtimeSet) (runtimeSnapshot, error) {
	paths := map[string]bool{}
	for _, path := range set.files {
		paths[path] = true
	}
	for _, path := range set.systemFiles {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			paths[path] = false
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("invalid system runtime file")
		}
		inCache, err := cacheContains(set.cacheDirs, path)
		if err != nil {
			return nil, err
		}
		paths[path] = !inCache
	}
	for i, dir := range append(append([]string(nil), set.dataDirs...), set.systemDirs...) {
		data := i < len(set.dataDirs)
		inCache := false
		for _, root := range set.cacheDirs {
			if dir == root {
				inCache = true
				break
			}
		}
		walkRoot := dir
		if data {
			var err error
			walkRoot, err = filepath.EvalSymlinks(dir)
			if err != nil {
				return nil, err
			}
		}
		err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(walkRoot, path)
			if err != nil {
				return err
			}
			logical := filepath.Join(dir, rel)
			shouldHash := !inCache && entry.Type().IsRegular()
			if data && entry.Type()&os.ModeSymlink != 0 {
				info, err := os.Stat(logical)
				if err != nil {
					return err
				}
				shouldHash = info.Mode().IsRegular()
			}
			paths[logical] = paths[logical] || shouldHash
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, path := range set.metadata {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, exists := paths[path]; !exists {
				paths[path] = false
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	result := make(runtimeSnapshot, 0, len(ordered))
	for _, path := range ordered {
		entry := runtimeEntry{path: path, hash: paths[path]}
		link, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, entry)
			continue
		}
		if err != nil {
			return nil, err
		}
		entry.link = link
		target, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) && !entry.hash {
			result = append(result, entry)
			continue
		}
		if err != nil {
			return nil, err
		}
		entry.target = target
		result = append(result, entry)
	}
	return result, nil
}

func sameInfo(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime()) && sameChangeTime(a, b)
}

func checkRuntimeSnapshot(set runtimeSet, expected runtimeSnapshot) error {
	actual, err := snapshotRuntime(set)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return errors.New("runtime files changed")
	}
	for i := range actual {
		a, b := actual[i], expected[i]
		if a.path != b.path || a.hash != b.hash || !sameInfo(a.link, b.link) || !sameInfo(a.target, b.target) {
			return errors.New("runtime files changed")
		}
	}
	return nil
}
