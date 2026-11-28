package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/stretchr/testify/require"
)

func TestDatabaseInputs_RejectsSnapshotRegistryAccessChange(t *testing.T) {
	work := fixture(t)
	require.NoError(t, testRun(context.Background(), "20990101", &bytes.Buffer{}))
	base := filepath.Join(work, "20990101")
	path := filepath.Join(base, "snapshot/sources/frameworks.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = []byte(strings.Replace(string(data), "access: licensed", "access: free", 1))
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = readDatabaseInputs(base)
	require.ErrorContains(t, err, "snapshot input changed")
}

func TestDatabaseInputs_RejectsChangedGoldenAndReview(t *testing.T) {
	work := fixture(t)
	header := "id,question,framework_code,version_label,citation_norm,expect_abstain,notes,adjudication,reason,expect_fail\n"
	old := header + "case,invented query,fw-alpha,v1,A-2,false,,accepted,reviewed,false\n"
	goldenInput(t, "golden.csv", old)
	goldenInput(t, "golden-review.yaml", goldenReview(digest([]byte(old))))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	var out bytes.Buffer
	for _, step := range []string{"snapshot", "parse", "relate", "chunk", "embed"} {
		require.NoError(t, runStep(context.Background(), base, step, DatabaseRequest{RunID: "invented"}, source, &out, fakeEmbedder{}, nil, RunOptions{}), step)
	}
	changed := strings.Replace(old, "invented query", "changed query", 1)
	require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/trachuan/golden.csv"), []byte(changed), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/trachuan/golden-review.yaml"), []byte(goldenReview(digest([]byte(changed)))), 0600))
	_, err = readDatabaseInputs(base)
	require.ErrorContains(t, err, "snapshot input changed")
}

func TestDatabaseInputs_UsesSameGoldenBytesForReviewAndCases(t *testing.T) {
	work := fixture(t)
	header := "id,question,framework_code,version_label,citation_norm,expect_abstain,notes,adjudication,reason,expect_fail\n"
	original := header + "case,invented query,fw-alpha,v1,A-2,false,,accepted,reviewed,false\n"
	goldenInput(t, "golden.csv", original)
	goldenInput(t, "golden-review.yaml", goldenReview(digest([]byte(original))))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	var out bytes.Buffer
	for _, step := range []string{"snapshot", "parse", "relate", "chunk", "embed"} {
		require.NoError(t, runStep(context.Background(), base, step, DatabaseRequest{RunID: "invented"}, source, &out, fakeEmbedder{}, nil, RunOptions{}), step)
	}
	frozen, err := readSnapshotParseInputs(base, nil)
	require.NoError(t, err)
	data, err := frozen.read("trachuan/golden.csv", true)
	require.NoError(t, err)
	require.Equal(t, original, string(data))
	changed := strings.Replace(original, "invented query", "changed query", 1)
	require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/trachuan/golden.csv"), []byte(changed), 0600))
	inputs, err := readDatabaseInputs(base, frozen)
	require.NoError(t, err)
	require.Equal(t, digest([]byte(original)), inputs.caseHash)
	require.Len(t, inputs.cases, 1)
	require.Equal(t, "invented query", inputs.cases[0].Question)
}

func TestArtifactReadsStayInSharedReader(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || (name == "snapshot.go" || name == "snapshot_read.go" || name == "step_read.go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(tree, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Join" {
					if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "filepath" {
						for _, arg := range call.Args {
							if literal, ok := arg.(*ast.BasicLit); ok && literal.Kind == token.STRING {
								value, err := strconv.Unquote(literal.Value)
								require.NoError(t, err)
								for _, step := range []string{"snapshot", "parse", "relate", "chunk", "embed", "build", "evaluate"} {
									if value == step || strings.HasPrefix(value, step+"/") {
										t.Errorf("direct %s artifact path in %s", step, name)
									}
								}
							}
						}
					}
				}
			}
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			require.NotContains(t, value, "snapshot/", name)
			return true
		})
	}
}

func TestSnapshotReader_OptionalAbsenceDoesNotSatisfyRequiredRead(t *testing.T) {
	work := fixture(t)
	runGoldenSnapshot(t)
	frozen, err := readSnapshotParseInputs(filepath.Join(work, "20990101"), nil)
	require.NoError(t, err)
	data, err := frozen.read("trachuan/golden.csv", true)
	require.NoError(t, err)
	require.Nil(t, data)
	_, err = frozen.read("trachuan/golden.csv", false)
	require.Error(t, err)
}

func TestSnapshotSeal_RejectsChangedReviewedInputs(t *testing.T) {
	fixture(t)
	original := "invented reviewed input\n"
	goldenInput(t, "golden.csv", original)
	goldenInput(t, "golden-review.yaml", goldenReview(digest([]byte(original))))
	source, _, err := prepare()
	require.NoError(t, err)
	parent := t.TempDir()
	tmp, target := filepath.Join(parent, "staged"), filepath.Join(parent, "snapshot")
	require.NoError(t, os.Mkdir(tmp, 0700))
	stage := newStagedOutputs(tmp)
	inputs, err := snapshotWithStage(context.Background(), tmp, source, nil, stage)
	require.NoError(t, err)
	changed := "changed reviewed input\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "trachuan/golden.csv"), []byte(changed), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "trachuan/golden-review.yaml"), []byte(goldenReview(digest([]byte(changed)))), 0600))
	require.ErrorContains(t, seal(tmp, target, stepRecipe("snapshot"), inputs, stage), "snapshot input changed")
	require.NoDirExists(t, target)
}

func TestChunkStep_DoesNotReadUnneededSourceBytes(t *testing.T) {
	work := fixture(t)
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	req := DatabaseRequest{RunID: "invented"}
	var out bytes.Buffer
	for _, step := range []string{"snapshot", "parse"} {
		require.NoError(t, runStep(context.Background(), base, step, req, source, &out, nil, nil, RunOptions{}))
	}
	require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/files/fw-alpha.json"), []byte("changed"), 0600))
	require.NoError(t, chunkStep(context.Background(), base, t.TempDir()))
}

func TestSnapshotResume_DoesNotRetainUnusedSourceBytes(t *testing.T) {
	work := fixture(t)
	data := bytes.Repeat([]byte("invented"), 512*1024)
	name := "unused-guide.pdf"
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), name), data, 0600))
	editRegistry(t, func(reg *registry.Registry) {
		reg.Frameworks[0].Files = append(reg.Frameworks[0].Files, registry.File{Path: name, Role: "guide", Format: "pdf", Size: int64(len(data)), SHA256: digest(data)})
	})
	runGoldenSnapshot(t)
	frozen, err := readSnapshotParseInputs(filepath.Join(work, "20990101"), nil)
	require.NoError(t, err)
	inputs, err := snapshotInputsFrozen(frozen)
	require.NoError(t, err)
	require.NoError(t, validateFrozenSnapshot(frozen, stepRecipe("snapshot"), inputs))
	require.NotContains(t, frozen.files, "files/"+name)
	require.Equal(t, digest(data), frozen.hashes["files/"+name])
	_, err = frozen.read("files/"+name, false)
	require.ErrorContains(t, err, "already hashed")
}

type parseProgressMutator struct {
	callback func() error
	err      error
}

func (w *parseProgressMutator) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "parse complete") && w.callback != nil {
		w.err = w.callback()
		w.callback = nil
		if w.err != nil {
			return 0, w.err
		}
	}
	return len(data), nil
}

func TestRun_RejectsSnapshotManifestAndAccessChangeAfterParse(t *testing.T) {
	work := fixture(t)
	base := filepath.Join(work, "20990101")
	out := &parseProgressMutator{callback: func() error {
		path := filepath.Join(base, "snapshot/sources/frameworks.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		changed := bytes.Replace(data, []byte("access: licensed"), []byte("access: free"), 1)
		if bytes.Equal(data, changed) {
			return fmt.Errorf("access marker not found")
		}
		if err = os.WriteFile(path, changed, 0600); err != nil {
			return err
		}
		path = filepath.Join(base, "snapshot/manifest.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m manifest
		if err = json.Unmarshal(raw, &m); err != nil {
			return err
		}
		m.Outputs["sources/frameworks.yaml"] = digest(changed)
		raw, err = json.Marshal(m)
		if err != nil {
			return err
		}
		return os.WriteFile(path, raw, 0600)
	}}
	err := Run(context.Background(), "20990101", out, fakeEmbedder{}, fakeDatabase{})
	require.NoError(t, out.err)
	require.Error(t, err)
}

func TestRun_RejectsParseArtifactChangeAfterParse(t *testing.T) {
	work := fixture(t)
	path := filepath.Join(work, "20990101/parse/controls.jsonl")
	out := &parseProgressMutator{callback: func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		changed := bytes.Replace(data, []byte("private body"), []byte("changed body"), 1)
		if bytes.Equal(data, changed) {
			return fmt.Errorf("body marker not found")
		}
		return os.WriteFile(path, changed, 0600)
	}}
	err := Run(context.Background(), "20990101", out, fakeEmbedder{}, fakeDatabase{})
	require.NoError(t, out.err)
	require.Error(t, err)
}

type changingDumpDatabase struct {
	fakeDatabase
	original string
	restored []byte
}

func (d *changingDumpDatabase) Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	if err := os.WriteFile(d.original, []byte("changed dump"), 0600); err != nil {
		return EvaluationResult{}, err
	}
	data, err := os.ReadFile(req.DumpPath)
	if err != nil {
		return EvaluationResult{}, err
	}
	d.restored = data
	return d.fakeDatabase.Evaluate(ctx, req)
}

func TestRun_EvaluationRejectsDumpChangedDuringRestore(t *testing.T) {
	work := fixture(t)
	db := &changingDumpDatabase{original: filepath.Join(work, "20990101/build/frameworks.dump")}
	err := Run(context.Background(), "20990101", &bytes.Buffer{}, fakeEmbedder{}, db)
	require.Error(t, err)
	require.Equal(t, "changed dump", string(db.restored))
	require.NoDirExists(t, filepath.Join(work, "20990101/evaluate"))
}

func TestRun_RejectsStepManifestAndArtifactChangeAfterParse(t *testing.T) {
	work := fixture(t)
	base := filepath.Join(work, "20990101")
	out := &parseProgressMutator{callback: func() error {
		path := filepath.Join(base, "parse/controls.jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		changed := bytes.Replace(data, []byte("private body"), []byte("changed body"), 1)
		if bytes.Equal(data, changed) {
			return fmt.Errorf("body marker not found")
		}
		if err = os.WriteFile(path, changed, 0600); err != nil {
			return err
		}
		path = filepath.Join(base, "parse/manifest.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m manifest
		if err = json.Unmarshal(raw, &m); err != nil {
			return err
		}
		m.Outputs["controls.jsonl"] = digest(changed)
		raw, err = json.Marshal(m)
		if err != nil {
			return err
		}
		return os.WriteFile(path, raw, 0600)
	}}
	err := Run(context.Background(), "20990101", out, fakeEmbedder{}, fakeDatabase{})
	require.NoError(t, out.err)
	require.Error(t, err)
}

func TestSeal_RejectsChangedOutputForEveryStep(t *testing.T) {
	for _, step := range []string{"parse", "relate", "chunk", "embed", "build", "evaluate"} {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			tmp, target := filepath.Join(root, "staged"), filepath.Join(root, step)
			require.NoError(t, os.Mkdir(tmp, 0700))
			stage := newStagedOutputs(tmp)
			require.NoError(t, writeFile(tmp, "output", []byte("validated"), stage))
			require.NoError(t, os.WriteFile(filepath.Join(tmp, "output"), []byte("changed"), 0600))
			require.ErrorContains(t, seal(tmp, target, stepRecipe(step), nil, stage), "staged output changed")
			require.NoDirExists(t, target)
		})
	}
}

func TestStepReader_RejectsEmbeddingChangedAfterManifestFreeze(t *testing.T) {
	work := fixture(t)
	require.NoError(t, testRun(context.Background(), "20990101", &bytes.Buffer{}))
	base := filepath.Join(work, "20990101")
	state := &runState{}
	_, err := state.capture(base, "embed")
	require.NoError(t, err)
	key := "fw:fw-alpha:v1"
	path := filepath.Join(base, "embed/artifacts", digest([]byte(key)), "vectors.f32")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	data[0] ^= 1
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = state.embedding(base, key)
	require.Error(t, err)
}

func TestStepReader_DoesNotRetainValidationOnlyEmbeddingReport(t *testing.T) {
	work := fixture(t)
	require.NoError(t, testRun(context.Background(), "20990101", &bytes.Buffer{}))
	base := filepath.Join(work, "20990101")
	state := &runState{}
	f, err := state.capture(base, "embed")
	require.NoError(t, err)
	require.NoError(t, state.validateStep(base, "embed", f.manifest.Recipe, f.manifest.Inputs))
	key := "fw:fw-alpha:v1"
	_, err = state.embedding(base, key)
	require.NoError(t, err)
	name := "artifacts/" + digest([]byte(key)) + "/report.json"
	_, retained := f.data.files[name]
	require.False(t, retained)
	require.Contains(t, f.data.hashes, name)
}

func TestStepPublicationOwnedByRunStep(t *testing.T) {
	var files []string
	for _, pattern := range []string{"*.go", "../evaluate/*.go", "../../../apps/corpus/internal/cli/frameworks*.go"} {
		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		files = append(files, matches...)
	}
	sealCalls := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(tree, func(node ast.Node) bool {
			if field, ok := node.(*ast.Field); ok {
				for _, ident := range field.Names {
					if ident.Name == "publish" {
						t.Errorf("publish callback remains in %s", name)
					}
				}
			}
			if literal, ok := node.(*ast.FuncLit); ok {
				ast.Inspect(literal.Body, func(inner ast.Node) bool {
					if call, ok := inner.(*ast.CallExpr); ok {
						if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "seal" {
							t.Errorf("seal called in callback in %s", name)
						}
					}
					return true
				})
			}
			return true
		})
		for _, declaration := range tree.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "seal" {
						sealCalls++
						if name != "run.go" || function.Name.Name != "runStep" {
							t.Errorf("seal called outside runStep in %s", name)
						}
					}
				}
				return true
			})
		}
	}
	require.Equal(t, 1, sealCalls)
}
