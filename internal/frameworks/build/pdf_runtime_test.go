package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/stretchr/testify/require"
)

type fakePDFRuntime struct {
	identity string
	calls    int
	failAt   int
	onVerify func(int)
}

func (f *fakePDFRuntime) RuntimeIdentity() string { return f.identity }
func (f *fakePDFRuntime) VerifyRuntime() error {
	f.calls++
	if f.onVerify != nil {
		f.onVerify(f.calls)
	}
	if f.calls == f.failAt {
		return errors.New("invented private runtime error")
	}
	return nil
}

func TestPDFRuntime_GuardsResume(t *testing.T) {
	work := fixture(t)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err := checkedDir(base)
	require.NoError(t, err)
	snapshot := filepath.Join(base, "snapshot")
	require.NoError(t, os.MkdirAll(filepath.Join(snapshot, "sources"), 0700))
	sourceRegistry, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	sourceRegistry = []byte(strings.Replace(string(sourceRegistry), "format: json", "format: pdf", 1))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "sources/frameworks.yaml"), sourceRegistry, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "manifest.json"), []byte("invented snapshot"), 0600))
	reg, err := readRegistry(filepath.Join(snapshot, "sources/frameworks.yaml"))
	require.NoError(t, err)
	identity := strings.Repeat("a", 64)
	old, err := pdfParseRecipe(reg, identity)
	require.NoError(t, err)
	inputs, err := stepInputs(base, "parse")
	require.NoError(t, err)
	tmp := filepath.Join(base, "tmp")
	require.NoError(t, os.Mkdir(tmp, 0700))
	require.NoError(t, writeFile(tmp, "artifact", []byte("invented")))
	require.NoError(t, seal(tmp, filepath.Join(base, "parse"), old, inputs))
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
	registryData = []byte(strings.Replace(string(registryData), "format: json", "format: pdf", 1))
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", registryData, 0600))
	source, _, err := prepare()
	require.NoError(t, err)
	base := filepath.Join(work, "20990101")
	require.NoError(t, os.MkdirAll(base, 0700))
	base, err = checkedDir(base)
	require.NoError(t, err)
	req := DatabaseRequest{RunID: "invented"}
	var out bytes.Buffer
	require.NoError(t, runStep(context.Background(), base, "snapshot", req, source, &out, nil, nil, RunOptions{}))
	runtime := &fakePDFRuntime{identity: strings.Repeat("a", 64), failAt: 2}
	err = runStep(context.Background(), base, "parse", req, source, &out, nil, nil, RunOptions{PDFRuntime: runtime})
	require.ErrorContains(t, err, "PDF runtime")
	require.NotContains(t, err.Error(), "invented private runtime error")
	require.Equal(t, 2, runtime.calls)
	require.NoDirExists(t, filepath.Join(base, "parse"))
}

func TestPDFParseRecipe_RuntimeIdentity(t *testing.T) {
	pdf := &registry.Registry{Frameworks: []registry.Framework{{Parser: "invented-pdf", ParseSource: "chosen.pdf", Files: []registry.File{{Path: "chosen.pdf", Role: "text", Format: "pdf"}}}}}
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
	require.NoError(t, writeFile(tmp, "artifact", []byte("invented")))
	require.NoError(t, seal(tmp, target, previous, nil))
	require.ErrorContains(t, validate(target, current, nil), "inputs or recipe changed")
}

func TestPDFParseRecipe_OnlyEnabledSelectedPDF(t *testing.T) {
	for _, f := range []registry.Framework{
		{Parser: "", Files: []registry.File{{Path: "x.pdf", Role: "text", Format: "pdf"}}},
		{Parser: "xlsx", ParseSource: "x.xlsx", Files: []registry.File{{Path: "x.xlsx", Role: "catalog", Format: "xlsx"}, {Path: "x.pdf", Role: "text", Format: "pdf"}}},
	} {
		got, err := pdfParseRecipe(&registry.Registry{Frameworks: []registry.Framework{f}}, strings.Repeat("a", 64))
		require.NoError(t, err)
		require.NotContains(t, got.Parameters, "pdf_runtime_sha256")
	}
}

func TestPDFParseRecipe_RejectsInvalidIdentity(t *testing.T) {
	pdf := &registry.Registry{Frameworks: []registry.Framework{{Parser: "pdf", ParseSource: "x", Files: []registry.File{{Path: "x", Format: "pdf", Role: "text"}}}}}
	for _, runtime := range []string{"", "../../private", strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		_, err := pdfParseRecipe(pdf, runtime)
		require.Error(t, err)
	}
}
