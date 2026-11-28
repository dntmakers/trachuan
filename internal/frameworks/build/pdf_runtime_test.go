package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/stretchr/testify/require"
)

type fakePDFRuntime struct {
	identity      string
	identityCalls int
	contexts      []context.Context
	calls         int
	failAt        int
	onVerify      func(int)
}

func (f *fakePDFRuntime) RuntimeIdentity() string { f.identityCalls++; return f.identity }
func (f *fakePDFRuntime) VerifyRuntime(ctx context.Context) error {
	f.contexts = append(f.contexts, ctx)
	f.calls++
	if f.onVerify != nil {
		f.onVerify(f.calls)
	}
	if f.calls == f.failAt {
		return errors.New("invented private runtime error")
	}
	return nil
}

func (f *fakePDFRuntime) Extract(_ context.Context, _ []byte, _, _ bool) (parse.PDFInputs, error) {
	return parse.PDFInputs{Layout: []string{"invented"}}, nil
}

func inventedTable() parse.Table {
	table := parse.Default()
	table["invented-pdf"] = parse.PDFParser{Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(inputs parse.PDFInputs, _ string, _ *int) (*parse.Result, error) {
		if len(inputs.Layout) != 1 {
			return nil, errors.New("missing layout")
		}
		return &parse.Result{Nodes: []parse.Node{
			{Citation: "A", CitationNorm: "A", Parent: -1, Ordinal: 0},
			{Citation: "A-2", CitationNorm: "A-2", Parent: 0, Ordinal: 1},
			{Citation: "A-2(1)", CitationNorm: "A-2(1)", Parent: 1, Ordinal: 2},
		}}, nil
	}}
	return table
}

func TestPDFRuntime_GuardsResume(t *testing.T) {
	work := fixture(t)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err := checkedDir(base)
	require.NoError(t, err)
	sourceRegistry, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	sourceRegistry = pdfFixtureRegistry(sourceRegistry)
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", sourceRegistry, 0600))
	source, _, err := prepare()
	require.NoError(t, err)
	snapshotDir := filepath.Join(base, "snapshot")
	snapshotTemp := filepath.Join(base, "snapshot-tmp")
	require.NoError(t, os.Mkdir(snapshotTemp, 0700))
	snapshotStage := newStagedOutputs(snapshotTemp)
	snapshotInput, err := snapshotWithStage(context.Background(), snapshotTemp, source, nil, snapshotStage)
	require.NoError(t, err)
	require.NoError(t, seal(snapshotTemp, snapshotDir, stepRecipe("snapshot"), snapshotInput, snapshotStage))
	reg, err := readRegistry(filepath.Join(snapshotDir, "sources/frameworks.yaml"))
	require.NoError(t, err)
	identity := strings.Repeat("a", 64)
	old, err := pdfParseRecipe(reg, identity)
	require.NoError(t, err)
	frozen, err := readSnapshotParseInputs(base, nil)
	require.NoError(t, err)
	inputs, err := stepInputs(base, "parse", &frozen.manifestHash)
	require.NoError(t, err)
	tmp := filepath.Join(base, "tmp")
	require.NoError(t, os.Mkdir(tmp, 0700))
	stage := newStagedOutputs(tmp)
	require.NoError(t, writeFile(tmp, "artifact", []byte("invented"), stage))
	require.NoError(t, seal(tmp, filepath.Join(base, "parse"), old, inputs, stage))
	var out bytes.Buffer
	req := DatabaseRequest{RunID: "invented"}
	err = runStep(context.Background(), base, "parse", req, "", &out, nil, nil, RunOptions{})
	require.ErrorContains(t, err, "PDF runtime identity")
	var nilRuntime *fakePDFRuntime
	err = runStep(context.Background(), base, "parse", req, "", &out, nil, nil, RunOptions{PDFRuntime: nilRuntime})
	require.ErrorContains(t, err, "PDF runtime identity")
	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelRuntime := &fakePDFRuntime{identity: identity, onVerify: func(int) { cancel() }}
	err = runStep(cancelled, base, "parse", req, "", &out, nil, nil, RunOptions{PDFRuntime: cancelRuntime})
	require.ErrorIs(t, err, context.Canceled)
	require.NotContains(t, out.String(), "parse skipped")
	err = runStep(context.Background(), base, "parse", req, "", &out, nil, nil, RunOptions{PDFRuntime: &fakePDFRuntime{identity: strings.Repeat("b", 64)}})
	require.ErrorContains(t, err, "inputs or recipe changed")
	verified := &fakePDFRuntime{identity: identity}
	err = runStep(context.Background(), base, "parse", req, "", &out, nil, nil, RunOptions{PDFRuntime: verified})
	require.NoError(t, err)
	require.Equal(t, 1, verified.calls)
	require.Contains(t, out.String(), "parse skipped")
}

func TestPDFRuntime_VerifiesAfterFreshParse(t *testing.T) {
	work := fixture(t)
	registryData, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	registryData = []byte(strings.Replace(string(pdfFixtureRegistry(registryData)), "iso27002-2022-pdf", "invented-pdf", 1))
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", registryData, 0600))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	req := DatabaseRequest{RunID: "invented"}
	var out bytes.Buffer
	require.NoError(t, runStep(context.Background(), base, "snapshot", req, source, &out, nil, nil, RunOptions{ParserTable: inventedTable()}))
	runtime := &fakePDFRuntime{identity: strings.Repeat("a", 64), failAt: 2}
	err = runStep(context.Background(), base, "parse", req, source, &out, nil, nil, RunOptions{PDFRuntime: runtime, ParserTable: inventedTable()})
	require.ErrorContains(t, err, "PDF runtime")
	require.NotContains(t, err.Error(), "invented private runtime error")
	require.Equal(t, 2, runtime.calls)
	require.NoDirExists(t, filepath.Join(base, "parse"))
}

func TestPDFParseRecipe_RuntimeIdentity(t *testing.T) {
	pdf := &registry.Registry{Frameworks: []registry.Framework{{Parser: "iso27002-2022-pdf", ParseSource: "chosen.pdf", Files: []registry.File{{Path: "chosen.pdf", Role: "text", Format: "pdf"}}}}}
	_, err := pdfParseRecipe(pdf, "")
	require.ErrorContains(t, err, "PDF runtime identity")
	identity := strings.Repeat("a", 64)
	current, err := pdfParseRecipe(pdf, identity)
	require.NoError(t, err)
	require.Equal(t, identity, current.Parameters["pdf_runtime_sha256"])
	previous, err := pdfParseRecipe(pdf, strings.Repeat("b", 64))
	require.NoError(t, err)
	base := t.TempDir()
	tmp, target := filepath.Join(base, "tmp"), filepath.Join(base, "parse")
	require.NoError(t, os.Mkdir(tmp, 0700))
	stage := newStagedOutputs(tmp)
	require.NoError(t, writeFile(tmp, "artifact", []byte("invented"), stage))
	require.NoError(t, seal(tmp, target, previous, nil, stage))
	require.ErrorContains(t, validate(target, current, nil), "inputs or recipe changed")
}

func TestPDFParseRecipe_OnlyEnabledSelectedPDF(t *testing.T) {
	for _, f := range []registry.Framework{
		{Parser: "", Files: []registry.File{{Path: "x.pdf", Role: "text", Format: "pdf"}}},
		{Parser: "csa-ccm-4-1-xlsx", ParseSource: "x.xlsx", Files: []registry.File{{Path: "x.xlsx", Role: "catalog", Format: "xlsx"}, {Path: "x.pdf", Role: "text", Format: "pdf"}}},
	} {
		got, err := pdfParseRecipe(&registry.Registry{Frameworks: []registry.Framework{f}}, strings.Repeat("a", 64))
		require.NoError(t, err)
		require.NotContains(t, got.Parameters, "pdf_runtime_sha256")
	}
}

func TestPDFParseRecipe_RejectsInvalidIdentity(t *testing.T) {
	pdf := &registry.Registry{Frameworks: []registry.Framework{{Parser: "iso27002-2022-pdf", ParseSource: "x", Files: []registry.File{{Path: "x", Format: "pdf", Role: "text"}}}}}
	for _, runtime := range []string{"", "../../private", strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		_, err := pdfParseRecipe(pdf, runtime)
		require.Error(t, err)
	}
}

func pdfFixtureRegistry(data []byte) []byte {
	s := strings.Replace(string(data), "parser: nist-oscal-800-53", "parser: iso27002-2022-pdf\n    parse_source: fw-alpha.json", 1)
	s = strings.Replace(s, "role: catalog", "role: text", 1)
	return []byte(strings.Replace(s, "format: json", "format: pdf", 1))
}

func TestPDFRuntime_InventedParserPassesDownstreamStages(t *testing.T) {
	work := fixture(t)
	data, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	data = []byte(strings.Replace(string(pdfFixtureRegistry(data)), "iso27002-2022-pdf", "invented-pdf", 1))
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", data, 0600))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	var out bytes.Buffer
	opts := RunOptions{ParserTable: inventedTable(), PDFRuntime: &fakePDFRuntime{identity: strings.Repeat("a", 64)}}
	for _, step := range []string{"snapshot", "parse", "relate", "chunk"} {
		require.NoError(t, runStep(context.Background(), base, step, DatabaseRequest{RunID: "invented"}, source, &out, nil, nil, opts), step)
	}
}

func TestPDFRuntime_CatalogDoesNotInspectRuntime(t *testing.T) {
	work := fixture(t)
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	runtime := &fakePDFRuntime{}
	opts := RunOptions{PDFRuntime: runtime}
	var out bytes.Buffer
	for _, step := range []string{"snapshot", "parse"} {
		require.NoError(t, runStep(context.Background(), base, step, DatabaseRequest{RunID: "invented"}, source, &out, nil, nil, opts))
	}
	require.Zero(t, runtime.identityCalls)
	require.Zero(t, runtime.calls)
}

func TestPDFParseRecipe_ChangesWithParserMode(t *testing.T) {
	table := inventedTable()
	reg := &registry.Registry{Frameworks: []registry.Framework{{Parser: "invented-pdf"}}}
	a, err := pdfParseRecipe(reg, strings.Repeat("a", 64), table)
	require.NoError(t, err)
	p := table["invented-pdf"]
	p.BBox = true
	table["invented-pdf"] = p
	b, err := pdfParseRecipe(reg, strings.Repeat("a", 64), table)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	base := t.TempDir()
	temp, target := filepath.Join(base, "tmp"), filepath.Join(base, "parse")
	require.NoError(t, os.Mkdir(temp, 0700))
	stage := newStagedOutputs(temp)
	require.NoError(t, writeFile(temp, "artifact", []byte("invented"), stage))
	require.NoError(t, seal(temp, target, a, nil, stage))
	require.ErrorContains(t, validate(target, b, nil), "inputs or recipe changed")
	p.BBox = false
	p.Revision = "2"
	table["invented-pdf"] = p
	c, err := pdfParseRecipe(reg, strings.Repeat("a", 64), table)
	require.NoError(t, err)
	require.ErrorContains(t, validate(target, c, nil), "inputs or recipe changed")
}

type runtimeWithoutExtraction struct{}

func (runtimeWithoutExtraction) RuntimeIdentity() string             { return strings.Repeat("a", 64) }
func (runtimeWithoutExtraction) VerifyRuntime(context.Context) error { return nil }

func TestPDFRuntime_RequiresExtraction(t *testing.T) {
	_, ok := any(runtimeWithoutExtraction{}).(PDFRuntime)
	require.False(t, ok)
}

func TestPDFParseRecipe_RejectsInvalidCatalogEntry(t *testing.T) {
	table := parse.Default()
	entry := table["nist-oscal-800-53"]
	entry.Catalog = nil
	table["nist-oscal-800-53"] = entry
	reg := &registry.Registry{Frameworks: []registry.Framework{{Parser: "nist-oscal-800-53"}}}
	_, err := pdfParseRecipe(reg, "", table)
	require.Error(t, err)
}

func TestPDFRuntime_FreezesRegistryAndParserTable(t *testing.T) {
	for _, mutate := range []string{"registry", "table", "titles", "source", "manifest"} {
		t.Run(mutate, func(t *testing.T) {
			work := fixture(t)
			data, err := os.ReadFile("sources/frameworks.yaml")
			require.NoError(t, err)
			data = []byte(strings.Replace(string(pdfFixtureRegistry(data)), "iso27002-2022-pdf", "invented-pdf", 1))
			require.NoError(t, os.WriteFile("sources/frameworks.yaml", data, 0600))
			source, _, err := prepare()
			require.NoError(t, err)
			base := filepath.Join(work, "20990101")
			require.NoError(t, os.MkdirAll(base, 0700))
			base, err = checkedDir(base)
			require.NoError(t, err)
			table := inventedTable()
			runtime := &fakePDFRuntime{identity: strings.Repeat("a", 64)}
			opts := RunOptions{ParserTable: table, PDFRuntime: runtime}
			var out bytes.Buffer
			req := DatabaseRequest{RunID: "invented"}
			require.NoError(t, runStep(context.Background(), base, "snapshot", req, source, &out, nil, nil, opts))
			runtime.onVerify = func(call int) {
				if call != 1 {
					return
				}
				switch mutate {
				case "registry":
					require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/sources/frameworks.yaml"), []byte("invalid"), 0600))
				case "manifest":
					require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/manifest.json"), []byte("invalid"), 0600))
				case "titles":
					require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/trachuan/titles.csv"), []byte("invalid"), 0600))
				case "source":
					require.NoError(t, os.WriteFile(filepath.Join(base, "snapshot/files/fw-alpha.json"), []byte("invalid"), 0600))
				default:
					entry := table["invented-pdf"]
					entry.Parse = func(parse.PDFInputs, string, *int) (*parse.Result, error) { return nil, errors.New("changed parser") }
					table["invented-pdf"] = entry
				}
			}
			require.NoError(t, runStep(context.Background(), base, "parse", req, source, &out, nil, nil, opts))
		})
	}
}

func TestPDFRuntime_RejectsInvalidTableBeforeResume(t *testing.T) {
	work := fixture(t)
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	var out bytes.Buffer
	req := DatabaseRequest{RunID: "invented"}
	for _, step := range []string{"snapshot", "parse"} {
		require.NoError(t, runStep(context.Background(), base, step, req, source, &out, nil, nil, RunOptions{}))
	}
	table := parse.Default()
	entry := table["nist-oscal-800-53"]
	entry.Catalog = nil
	table["nist-oscal-800-53"] = entry
	err = runStep(context.Background(), base, "parse", req, source, &out, nil, nil, RunOptions{ParserTable: table})
	require.ErrorContains(t, err, "invalid parser configuration")
}

func TestPDFRuntime_RejectsChangedSnapshotInputs(t *testing.T) {
	for _, name := range []string{"sources/frameworks.yaml", "trachuan/titles.csv", "files/fw-alpha.json"} {
		t.Run(name, func(t *testing.T) {
			work := fixture(t)
			source, _, err := prepare()
			require.NoError(t, err)
			base := filepath.Join(work, "20990101")
			require.NoError(t, os.MkdirAll(base, 0700))
			base, err = checkedDir(base)
			require.NoError(t, err)
			var out bytes.Buffer
			req := DatabaseRequest{RunID: "invented"}
			require.NoError(t, runStep(context.Background(), base, "snapshot", req, source, &out, nil, nil, RunOptions{}))
			path := filepath.Join(base, "snapshot", name)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, append(data, '\n'), 0600))
			err = runStep(context.Background(), base, "parse", req, source, &out, nil, nil, RunOptions{})
			require.ErrorContains(t, err, "snapshot input changed")
		})
	}
}

func TestPDFRuntime_VerificationReceivesStepContext(t *testing.T) {
	work := fixture(t)
	registryData, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	registryData = []byte(strings.Replace(string(pdfFixtureRegistry(registryData)), "iso27002-2022-pdf", "invented-pdf", 1))
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", registryData, 0600))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "step")
	runtime := &fakePDFRuntime{identity: strings.Repeat("a", 64)}
	opts := RunOptions{PDFRuntime: runtime, ParserTable: inventedTable()}
	var out bytes.Buffer
	req := DatabaseRequest{RunID: "invented"}
	require.NoError(t, runStep(ctx, base, "snapshot", req, source, &out, nil, nil, opts))
	require.NoError(t, runStep(ctx, base, "parse", req, source, &out, nil, nil, opts))
	require.Len(t, runtime.contexts, 2)
	for _, got := range runtime.contexts {
		require.Equal(t, "step", got.Value(contextKey{}))
	}
	runtime.contexts = nil
	require.NoError(t, runStep(ctx, base, "parse", req, source, &out, nil, nil, opts))
	require.Len(t, runtime.contexts, 1)
	require.Equal(t, "step", runtime.contexts[0].Value(contextKey{}))
}
