package pdftext

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runtimeDataPaths(cellar, poppler string) (runtimeSet, error) {
	var err error
	cellar, err = filepath.EvalSymlinks(cellar)
	if err != nil {
		return runtimeSet{}, err
	}
	seen := map[string]bool{}
	var set runtimeSet
	var visit func(string) error
	visit = func(path string) error {
		real, links, directories, err := traceRuntimePath(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cellar, real)
		if err != nil || filepath.IsAbs(rel) {
			return errors.New("invalid data path")
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 3 || parts[0] == ".." || parts[0] == "." || parts[2] != "share" {
			return errors.New("invalid data keg")
		}
		set.metadata = append(set.metadata, path)
		set.metadata = append(set.metadata, directories...)
		set.metadata = append(set.metadata, links...)
		info, err := os.Stat(real)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				return errors.New("invalid data file")
			}
			set.files = append(set.files, links...)
			set.files = append(set.files, real)
			return nil
		}
		set.dataDirs = append(set.dataDirs, links...)
		if seen[real] {
			return nil
		}
		seen[real] = true
		set.dataDirs = append(set.dataDirs, real)
		return filepath.WalkDir(real, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return visit(path)
			}
			return nil
		})
	}
	if err := visit(filepath.Join(poppler, "share", "poppler")); err != nil {
		return runtimeSet{}, err
	}
	sort.Strings(set.files)
	sort.Strings(set.dataDirs)
	sort.Strings(set.metadata)
	return set, nil
}
