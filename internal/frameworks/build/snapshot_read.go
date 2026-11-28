package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

func checkSnapshotTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return hide("inspect snapshot", walkErr)
		}
		if entry.IsDir() {
			_, err := checkedDir(path)
			return err
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("snapshot contains non-regular file")
		}
		return nil
	})
}

type snapshotParseInputs struct {
	stage        *stagedOutputs
	state        *runState
	dir          string
	manifestHash string
	manifestData []byte
	outputs      map[string]string
	manifest     manifest
	files        map[string][]byte
	hashes       map[string]string
	loaded       map[string]bool
	table        parse.Table
	registry     *registry.Registry
	titles       []byte
	sources      map[string][]byte
}

func (s *snapshotParseInputs) read(name string, optional bool) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("invalid snapshot input")
	}
	if _, hashed := s.hashes[name]; hashed {
		return nil, fmt.Errorf("snapshot input already hashed")
	}
	if s.loaded[name] {
		if _, exists := s.outputs[name]; !exists && !optional {
			return nil, fmt.Errorf("snapshot input missing")
		}
		return s.files[name], nil
	}
	expected, exists := s.outputs[name]
	inputData, readErr := readFile(filepath.Join(s.dir, name))
	if optional && !exists && errors.Is(readErr, fs.ErrNotExist) {
		s.loaded[name] = true
		return nil, nil
	}
	if readErr != nil {
		return nil, readErr
	}
	if !exists || digest(inputData) != expected {
		return nil, fmt.Errorf("snapshot input changed")
	}
	if inputData == nil {
		inputData = []byte{}
	}
	s.loaded[name] = true
	s.files[name] = inputData
	return inputData, nil
}

func (s *snapshotParseInputs) hash(name string, optional bool) (*string, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("invalid snapshot input")
	}
	if s.loaded[name] {
		if _, exists := s.outputs[name]; !exists {
			if optional {
				return nil, nil
			}
			return nil, fmt.Errorf("snapshot input missing")
		}
		value := digest(s.files[name])
		return &value, nil
	}
	if value, ok := s.hashes[name]; ok {
		return &value, nil
	}
	expected, exists := s.outputs[name]
	path := filepath.Join(s.dir, name)
	parent, err := checkedDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	info, err := os.Lstat(path)
	if optional && !exists && errors.Is(err, fs.ErrNotExist) {
		s.loaded[name] = true
		return nil, nil
	}
	if err != nil {
		return nil, hide("inspect artifact", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file")
	}
	if !exists {
		return nil, fmt.Errorf("snapshot input changed")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, hide("open artifact", err)
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil {
		return nil, hide("stat artifact", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file")
	}
	sum := sha256.New()
	if _, err = io.Copy(sum, file); err != nil {
		return nil, hide("read artifact", err)
	}
	value := fmt.Sprintf("%x", sum.Sum(nil))
	if value != expected {
		return nil, fmt.Errorf("snapshot input changed")
	}
	s.hashes[name] = value
	return &value, nil
}

func readSnapshotParseInputs(base string, table parse.Table) (*snapshotParseInputs, error) {
	dir := filepath.Join(base, "snapshot")
	data, err := readFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	return readSnapshotWithManifest(base, table, data)
}

func readSnapshotWithManifest(base string, table parse.Table, data []byte) (*snapshotParseInputs, error) {
	dir := filepath.Join(base, "snapshot")
	if err := checkSnapshotTree(dir); err != nil {
		return nil, err
	}
	var err error
	var m manifest
	if decodeErr := json.Unmarshal(data, &m); decodeErr != nil {
		return nil, hide("invalid snapshot manifest", decodeErr)
	}
	result := &snapshotParseInputs{dir: dir, manifestHash: digest(data), manifestData: data, outputs: m.Outputs, manifest: m, files: map[string][]byte{}, hashes: map[string]string{}, loaded: map[string]bool{}, table: table, sources: map[string][]byte{}}
	data, err = result.read("sources/frameworks.yaml", false)
	if err != nil {
		return nil, err
	}
	result.registry, err = registry.ParseWithTable(bytes.NewReader(data), table)
	if err != nil {
		return nil, hide("validate registry", err)
	}
	return result, nil
}

func (result *snapshotParseInputs) loadParseInputs() error {
	var err error
	var data []byte
	result.titles, err = result.read("trachuan/titles.csv", true)
	if err != nil {
		return err
	}
	for _, version := range releaseVersions(result.registry) {
		file, selectErr := version.SelectedSourceWithTable(result.table)
		if selectErr != nil {
			return hide("select source", selectErr)
		}
		if _, exists := result.sources[file.Path]; exists {
			continue
		}
		data, err = result.read("files/"+file.Path, false)
		if err != nil {
			return err
		}
		if int64(len(data)) != file.Size || digest(data) != file.SHA256 {
			return fmt.Errorf("source integrity mismatch")
		}
		result.sources[file.Path] = data
	}
	return nil
}

func (s *snapshotParseInputs) artifactState() *runState {
	if s.state == nil {
		s.state = &runState{}
	}
	return s.state
}

func snapshotForStep(base string, snapshots ...*snapshotParseInputs) (*snapshotParseInputs, error) {
	if len(snapshots) > 0 && snapshots[0] != nil {
		return snapshots[0], nil
	}
	return readSnapshotParseInputs(base, nil)
}

func snapshotInputsFrozen(frozen *snapshotParseInputs) (map[string]*string, error) {
	csv, err := frozen.read("trachuan/golden.csv", true)
	if err != nil {
		return nil, err
	}
	review, err := frozen.read("trachuan/golden-review.yaml", true)
	if err != nil {
		return nil, err
	}
	if err = validateGoldenReviewData(csv, review); err != nil {
		return nil, err
	}
	names := append([]string{"sources/frameworks.yaml", "gap-kinds.json"}, optionalSnapshotInputs()...)
	for _, v := range releaseVersions(frozen.registry) {
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
		value, readErr := frozen.hash(name, optional[name])
		if readErr != nil {
			return nil, readErr
		}
		inputs[name] = value
	}
	return inputs, nil
}

func validateFrozenSnapshot(frozen *snapshotParseInputs, expected recipe, inputs map[string]*string) error {
	a, err := json.Marshal(expected)
	if err != nil {
		return hide("encode recipe", err)
	}
	b, err := json.Marshal(frozen.manifest.Recipe)
	if err != nil {
		return hide("encode recipe", err)
	}
	if !equalJSON(a, b) || !reflect.DeepEqual(inputs, frozen.manifest.Inputs) {
		return fmt.Errorf("inputs or recipe changed")
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(frozen.dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return hide("inspect outputs", walkErr)
		}
		if entry.IsDir() {
			_, checkErr := checkedDir(path)
			return checkErr
		}
		name, relErr := filepath.Rel(frozen.dir, path)
		if relErr != nil {
			return hide("resolve output", relErr)
		}
		name = filepath.ToSlash(name)
		if name == "manifest.json" {
			return nil
		}
		if _, exists := frozen.outputs[name]; !exists {
			return fmt.Errorf("output validation failed")
		}
		if _, readErr := frozen.hash(name, false); readErr != nil {
			return readErr
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) == 0 || len(seen) != len(frozen.outputs) {
		return fmt.Errorf("output validation failed")
	}
	return nil
}
