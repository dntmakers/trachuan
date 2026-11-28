package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

func readRegistry(path string, tables ...parse.Table) (*registry.Registry, error) {
	var table parse.Table
	if len(tables) > 0 {
		table = tables[0]
	}
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	reg, err := registry.ParseWithTable(bytes.NewReader(data), table)
	if err != nil {
		return nil, hide("validate registry", err)
	}
	return reg, nil
}

func releaseVersions(reg *registry.Registry) []registry.Framework {
	var versions []registry.Framework
	for _, v := range reg.Frameworks {
		if v.Parser != "" {
			versions = append(versions, v)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		a, b := versions[i], versions[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		ad, bd := "", ""
		if a.Released != nil {
			ad = *a.Released
		}
		if b.Released != nil {
			bd = *b.Released
		}
		if ad != bd {
			return ad < bd
		}
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.Version < b.Version
	})
	return versions
}

func snapshot(ctx context.Context, dir, sourceDir string, tables ...parse.Table) (map[string]*string, error) {
	var table parse.Table
	if len(tables) > 0 {
		table = tables[0]
	}
	return snapshotWithStage(ctx, dir, sourceDir, table, nil)
}

func snapshotWithStage(ctx context.Context, dir, sourceDir string, table parse.Table, stage *stagedOutputs) (map[string]*string, error) {
	data, err := readFile("sources/frameworks.yaml")
	if err != nil {
		return nil, err
	}
	reg, err := registry.ParseWithTable(bytes.NewReader(data), table)
	if err != nil {
		return nil, hide("validate registry", err)
	}
	if len(releaseVersions(reg)) == 0 {
		return nil, fmt.Errorf("no release versions")
	}
	if err = writeFile(dir, "sources/frameworks.yaml", data, stage); err != nil {
		return nil, err
	}
	if err = writeJSON(dir, "gap-kinds.json", gapKinds(), stage); err != nil {
		return nil, err
	}
	for _, v := range releaseVersions(reg) {
		for _, file := range v.Files {
			if contextErr := ctx.Err(); contextErr != nil {
				return nil, contextErr
			}
			name, readErr := checkedSourceFile(sourceDir, file.Path)
			if readErr != nil {
				return nil, readErr
			}
			if err = os.Setenv("FRAMEWORKS_SOURCE_DIR", sourceDir); err != nil {
				return nil, hide("set source directory", err)
			}
			original := file.Path
			file.Path = name
			registered := registry.Registry{Frameworks: []registry.Framework{{Files: []registry.File{file}}}}
			source, readErr := registered.Read(name)
			if readErr != nil {
				return nil, hide("read registered source", readErr)
			}
			if err = writeFile(dir, "files/"+original, source, stage); err != nil {
				return nil, err
			}
		}
	}
	if err = snapshotPublicFloors(dir, stage); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(sourceDir)
	if err != nil {
		return nil, hide("open private inputs", err)
	}
	defer func() { _ = root.Close() }()
	for _, name := range optionalPrivateInputs() {
		checked, err := checkedSourceFile(sourceDir, name)
		if err != nil {
			return nil, err
		}
		info, err := root.Stat(checked)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, hide("inspect private input", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("private input is not a regular file")
		}
		data, err := root.ReadFile(checked)
		if err != nil {
			return nil, hide("read private input", err)
		}
		if err := writeFile(dir, name, data, stage); err != nil {
			return nil, err
		}
	}
	if err := writeRows(dir, "gaps.jsonl", []Gap{}, stage); err != nil {
		return nil, err
	}
	return snapshotInputs(dir, table)
}

func checkedSourceFile(source, name string) (string, error) {
	path, err := realPath(filepath.Join(source, name))
	if err != nil {
		return "", err
	}
	parent, err := checkedDir(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if !contained(path, source) {
		return "", fmt.Errorf("source file outside source directory")
	}
	name, err = filepath.Rel(source, path)
	if err != nil {
		return "", hide("resolve source file", err)
	}
	return filepath.ToSlash(name), nil
}

func snapshotInputs(dir string, tables ...parse.Table) (map[string]*string, error) {
	if err := checkSnapshotTree(dir); err != nil {
		return nil, err
	}
	var table parse.Table
	if len(tables) > 0 {
		table = tables[0]
	}
	cache := map[string][]byte{}
	loaded := map[string]bool{}
	read := func(name string, optional bool) ([]byte, error) {
		if loaded[name] {
			return cache[name], nil
		}
		data, err := readFile(filepath.Join(dir, name))
		if optional && errors.Is(err, fs.ErrNotExist) {
			loaded[name] = true
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		loaded[name] = true
		cache[name] = data
		return data, nil
	}
	csv, err := read("trachuan/golden.csv", true)
	if err != nil {
		return nil, err
	}
	review, err := read("trachuan/golden-review.yaml", true)
	if err != nil {
		return nil, err
	}
	if err = validateGoldenReviewData(csv, review); err != nil {
		return nil, err
	}
	data, err := read("sources/frameworks.yaml", false)
	if err != nil {
		return nil, err
	}
	reg, err := registry.ParseWithTable(bytes.NewReader(data), table)
	if err != nil {
		return nil, hide("validate registry", err)
	}
	names := append([]string{"sources/frameworks.yaml", "gap-kinds.json"}, optionalSnapshotInputs()...)
	for _, v := range releaseVersions(reg) {
		for _, file := range v.Files {
			names = append(names, "files/"+file.Path)
		}
	}
	optional := map[string]bool{}
	for _, name := range optionalSnapshotInputs() {
		optional[name] = true
	}
	inputs := map[string]*string{}
	for _, name := range names {
		data, readErr := read(name, optional[name])
		if readErr != nil {
			return nil, readErr
		}
		if data == nil && optional[name] {
			inputs[name] = nil
			continue
		}
		hash := digest(data)
		inputs[name] = &hash
	}
	return inputs, nil
}

func optionalSnapshotInputs() []string {
	return append(optionalPrivateInputs(), "sources/frameworks-floors.yaml")
}

func optionalPrivateInputs() []string {
	return []string{"trachuan/titles.csv", "trachuan/ocr-waivers.yaml", "trachuan/golden.csv", "trachuan/golden-review.yaml", "trachuan/floors.yaml", "trachuan/previous-report.json"}
}

func snapshotPublicFloors(dir string, stage *stagedOutputs) error {
	raw, err := optionalArtifact("sources/frameworks-floors.yaml")
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	return writeFile(dir, "sources/frameworks-floors.yaml", raw, stage)
}
