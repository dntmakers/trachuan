package build

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type frozenStep struct {
	raw      []byte
	hash     string
	manifest manifest
	data     *snapshotParseInputs
}

func (s *runState) capture(base, step string) (*frozenStep, error) {
	if s.steps == nil {
		s.steps = map[string]*frozenStep{}
	}
	if prior := s.steps[step]; prior != nil {
		return prior, nil
	}
	raw, err := readFile(filepath.Join(base, step, "manifest.json"))
	if err != nil {
		return nil, err
	}
	return s.captureBytes(base, step, raw)
}

func (s *runState) captureBytes(base, step string, raw []byte) (*frozenStep, error) {
	if s.steps == nil {
		s.steps = map[string]*frozenStep{}
	}
	if prior := s.steps[step]; prior != nil {
		return prior, nil
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, hide("invalid step manifest", err)
	}
	f := &frozenStep{raw: append([]byte(nil), raw...), hash: digest(raw), manifest: m}
	f.data = &snapshotParseInputs{dir: filepath.Join(base, step), outputs: m.Outputs, files: map[string][]byte{}, hashes: map[string]string{}, loaded: map[string]bool{}}
	s.steps[step] = f
	return f, nil
}

func (s *runState) read(base, step, name string) ([]byte, error) {
	f, err := s.capture(base, step)
	if err != nil {
		return nil, err
	}
	return f.data.read(name, false)
}

func (s *runState) hash(base, step, name string) (string, error) {
	f, err := s.capture(base, step)
	if err != nil {
		return "", err
	}
	value, err := f.data.hash(name, false)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", fmt.Errorf("step artifact missing")
	}
	return *value, nil
}

func retainedStepArtifact(step, name string) bool {
	switch step {
	case "parse":
		return name == "controls.jsonl" || name == "catalogs.jsonl" || name == "gaps.jsonl"
	case "relate":
		return name == "edges.jsonl" || name == "gaps.jsonl"
	case "chunk":
		return name == "chunks.jsonl" || name == "gaps.jsonl"
	case "embed":
		if name == "gaps.jsonl" {
			return true
		}
		parts := strings.Split(name, "/")
		if len(parts) != 3 || parts[0] != "artifacts" {
			return false
		}
		switch parts[2] {
		case "index.json", "vectors.f32", "MANIFEST.sha256":
			return true
		}
		return false
	case "build":
		return name == "build.json" || name == "cases.jsonl" || name == "calibration.json"
	default:
		return false
	}
}

func (s *runState) validateStep(base, step string, expected recipe, inputs map[string]*string) error {
	f, err := s.capture(base, step)
	if err != nil {
		return err
	}
	a, err := json.Marshal(expected)
	if err != nil {
		return hide("encode recipe", err)
	}
	b, err := json.Marshal(f.manifest.Recipe)
	if err != nil {
		return hide("encode recipe", err)
	}
	if !equalJSON(a, b) || !reflect.DeepEqual(inputs, f.manifest.Inputs) {
		return fmt.Errorf("inputs or recipe changed")
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(f.data.dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return hide("inspect outputs", walkErr)
		}
		if entry.IsDir() {
			_, checkErr := checkedDir(path)
			return checkErr
		}
		name, relErr := filepath.Rel(f.data.dir, path)
		if relErr != nil {
			return hide("resolve output", relErr)
		}
		name = filepath.ToSlash(name)
		if name == "manifest.json" {
			return nil
		}
		if _, ok := f.manifest.Outputs[name]; !ok {
			return fmt.Errorf("output validation failed")
		}
		if retainedStepArtifact(step, name) {
			_, err = f.data.read(name, false)
		} else {
			_, err = f.data.hash(name, false)
		}
		if err != nil {
			return fmt.Errorf("output validation failed")
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) == 0 || len(seen) != len(f.manifest.Outputs) {
		return fmt.Errorf("output validation failed")
	}
	return nil
}

func readStepRows[T any](frozen *snapshotParseInputs, base, step, name string) ([]T, error) {
	raw, err := frozen.artifactState().read(base, step, name)
	if err != nil {
		return nil, err
	}
	return decodeRows[T](raw)
}

func embeddingFiles(fetch func(string) ([]byte, error), hashReport func() (string, error)) (EmbedArtifact, error) {
	data := map[string][]byte{}
	var lines []string
	for _, name := range []string{"index.json", "vectors.f32"} {
		raw, err := fetch(name)
		if err != nil {
			return EmbedArtifact{}, err
		}
		data[name] = raw
		lines = append(lines, fmt.Sprintf("%s  %s\n", digest(raw), name))
	}
	reportHash, err := hashReport()
	if err != nil {
		return EmbedArtifact{}, err
	}
	lines = append(lines, fmt.Sprintf("%s  report.json\n", reportHash))
	sort.Strings(lines)
	expected, err := fetch("MANIFEST.sha256")
	if err != nil {
		return EmbedArtifact{}, err
	}
	if string(expected) != strings.Join(lines, "") {
		return EmbedArtifact{}, fmt.Errorf("embedding artifact manifest mismatch")
	}
	return EmbedArtifact{Index: data["index.json"], Vectors: data["vectors.f32"]}, nil
}

func (s *runState) embedding(base, key string) (EmbedArtifact, error) {
	prefix := "artifacts/" + digest([]byte(key)) + "/"
	f, err := s.capture(base, "embed")
	if err != nil {
		return EmbedArtifact{}, err
	}
	count := 0
	for name := range f.manifest.Outputs {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	if count != 4 {
		return EmbedArtifact{}, fmt.Errorf("invalid embedding artifact")
	}
	return embeddingFiles(func(name string) ([]byte, error) { return s.read(base, "embed", prefix+name) }, func() (string, error) { return s.hash(base, "embed", prefix+"report.json") })
}

func stagedEmbedding(dir, key string) (EmbedArtifact, error) {
	path := filepath.Join(dir, "artifacts", digest([]byte(key)))
	info, err := os.Lstat(path)
	if err != nil {
		return EmbedArtifact{}, hide("inspect embedding artifact", err)
	}
	if !info.IsDir() {
		return EmbedArtifact{}, fmt.Errorf("invalid embedding artifact")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return EmbedArtifact{}, hide("read embedding artifact", err)
	}
	if len(entries) != 4 {
		return EmbedArtifact{}, fmt.Errorf("invalid embedding artifact")
	}
	return embeddingFiles(func(name string) ([]byte, error) { return readFile(filepath.Join(path, name)) }, func() (string, error) {
		hash, _, err := hashStagedOutput(filepath.Join(path, "report.json"))
		return hash, err
	})
}

func embeddingCount(base, dir string, state *runState, staged bool) (int, error) {
	if staged {
		entries, err := os.ReadDir(filepath.Join(dir, "artifacts"))
		if err != nil {
			return 0, err
		}
		return len(entries), nil
	}
	f, err := state.capture(base, "embed")
	if err != nil {
		return 0, err
	}
	groups := map[string]bool{}
	for name := range f.manifest.Outputs {
		if !strings.HasPrefix(name, "artifacts/") {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) != 3 || parts[1] == "" {
			return 0, fmt.Errorf("invalid embedding artifact")
		}
		groups[parts[1]] = true
	}
	return len(groups), nil
}

func (s *runState) artifactPath(base, step, name string) (string, error) {
	if _, err := s.hash(base, step, name); err != nil {
		return "", err
	}
	return filepath.Join(base, step, name), nil
}

func (s *runState) verifyExternal(base, step, name string) error {
	f, err := s.capture(base, step)
	if err != nil {
		return err
	}
	expected, ok := f.manifest.Outputs[name]
	if !ok {
		return fmt.Errorf("step artifact missing")
	}
	copy := &snapshotParseInputs{dir: f.data.dir, outputs: map[string]string{name: expected}, files: map[string][]byte{}, hashes: map[string]string{}, loaded: map[string]bool{}}
	_, err = copy.hash(name, false)
	return err
}

func (s *runState) artifactSize(base, step, name string) (int64, error) {
	path, err := s.artifactPath(base, step, name)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return 0, hide("inspect artifact", err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("artifact is not a regular file")
	}
	return info.Size(), nil
}

func (s *runState) capturePublished(base, step string, expected recipe, inputs map[string]*string) error {
	f, err := s.capture(base, step)
	if err != nil {
		return err
	}
	want, err := json.Marshal(expected)
	if err != nil {
		return hide("encode recipe", err)
	}
	actual, err := json.Marshal(f.manifest.Recipe)
	if err != nil {
		return hide("encode recipe", err)
	}
	if !equalJSON(want, actual) || !reflect.DeepEqual(inputs, f.manifest.Inputs) {
		return fmt.Errorf("inputs or recipe changed")
	}
	return nil
}

func stepInputs(base, step string, snapshotHash *string, states ...*runState) (map[string]*string, error) {
	inputs := map[string]*string{}
	deps := []string{"snapshot"}
	if step == "relate" || step == "chunk" {
		deps = append(deps, "parse")
	}
	if step == "embed" {
		deps = append(deps, "chunk")
	}
	if step == "build" || step == "evaluate" {
		deps = append(deps, "parse", "relate", "chunk", "embed")
	}
	if step == "evaluate" {
		deps = append(deps, "build")
	}
	for _, dep := range deps {
		name := dep + "/manifest.json"
		if dep == "snapshot" {
			if snapshotHash == nil {
				return nil, fmt.Errorf("snapshot hash missing")
			}
			inputs[name] = snapshotHash
			continue
		}
		var state *runState
		if len(states) > 0 {
			state = states[0]
		}
		if state == nil {
			state = &runState{}
		}
		frozen, err := state.capture(base, dep)
		if err != nil {
			return nil, err
		}
		inputs[name] = &frozen.hash
	}
	return inputs, nil
}
