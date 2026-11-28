package build

import (
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

func TestParseSource_PDFRequiresExtractor(t *testing.T) {
	v := testPDFVersion("iso27002-2022-pdf", []byte("invented"), 3)
	_, err := ParseSource(context.Background(), v, []byte("invented"), nil)
	require.ErrorContains(t, err, "PDF extractor")
	var typedNil *recordingExtractor
	_, err = ParseSource(context.Background(), v, []byte("invented"), typedNil)
	require.ErrorContains(t, err, "PDF extractor")
}

type recordingExtractor struct {
	calls int
	fail  bool
}

func (r *recordingExtractor) Extract(_ context.Context, data []byte, layout, bbox bool) (parse.PDFInputs, error) {
	r.calls++
	if r.fail {
		return parse.PDFInputs{}, errors.New("private detail")
	}
	if len(data) == 0 || !layout || bbox {
		return parse.PDFInputs{}, errors.New("unexpected extraction")
	}
	return parse.PDFInputs{Layout: []string{"invented"}}, nil
}

func TestParseSource_UsesExtractorAndHidesFailures(t *testing.T) {
	extractor := &recordingExtractor{}
	v := testPDFVersion("iso27002-2022-pdf", []byte("invented"), 3)
	_, err := ParseSource(context.Background(), v, []byte("invented"), extractor)
	require.Error(t, err)
	require.Equal(t, 1, extractor.calls)
	extractor.fail = true
	_, err = ParseSource(context.Background(), v, []byte("invented"), extractor)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private detail")
}

func TestParseSource_CatalogDoesNotExtract(t *testing.T) {
	extractor := &recordingExtractor{fail: true}
	_, err := ParseSource(context.Background(), registry.Framework{Parser: "nist-oscal-800-53"}, []byte("invented"), extractor)
	require.Error(t, err)
	require.Zero(t, extractor.calls)
}

func TestInjectedParserControlsRecipeAndExtraction(t *testing.T) {
	table := inventedTable()
	v := testPDFVersion("invented-pdf", []byte("invented"), 3)
	reg := &registry.Registry{Frameworks: []registry.Framework{v}}
	_, err := pdfParseRecipe(reg, "", table)
	require.ErrorContains(t, err, "PDF runtime identity")
	extractor := &recordingExtractor{}
	result, err := ParseSourceWithTable(context.Background(), v, []byte("invented"), extractor, table)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 3)
	require.Equal(t, 1, extractor.calls)
}

func TestTitleCommandsUseInjectedPDFParser(t *testing.T) {
	fixture(t)
	data, err := os.ReadFile("sources/frameworks.yaml")
	require.NoError(t, err)
	data = []byte(strings.Replace(string(pdfFixtureRegistry(data)), "iso27002-2022-pdf", "invented-pdf", 1))
	require.NoError(t, os.WriteFile("sources/frameworks.yaml", data, 0600))
	extractor := &recordingExtractor{}
	input := filepath.Join(t.TempDir(), "input.csv")
	csv := "framework_code,version_label,citation_norm,title\nfw-alpha,v1,A,Family\nfw-alpha,v1,A-2,Control\nfw-alpha,v1,A-2(1),Child\n"
	require.NoError(t, os.WriteFile(input, []byte(csv), 0600))
	require.NoError(t, ImportTitlesWithOptions(context.Background(), input, extractor, inventedTable()))
	require.Equal(t, 1, extractor.calls)
	extractor.calls = 0
	require.NoError(t, CheckTitlesWithOptions(context.Background(), extractor, inventedTable()))
	require.Equal(t, 1, extractor.calls)
}

func TestParseSource_RejectsParserAbsentFromTable(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"a","title":"family","controls":[]}]}}`)
	count := 1
	v := registry.Framework{Parser: "nist-oscal-800-53", ExpectedControls: &count}
	_, err := ParseSourceWithTable(context.Background(), v, data, nil, parse.Table{})
	require.Error(t, err)
}

func TestParseSource_RejectsMissingOrWrongExpectedCount(t *testing.T) {
	table := inventedTable()
	for _, expected := range []*int{nil, new(int)} {
		v := testPDFVersion("invented-pdf", []byte("invented"), 3)
		v.ExpectedControls = expected
		_, err := ParseSourceWithTable(context.Background(), v, []byte("invented"), &recordingExtractor{}, table)
		require.Error(t, err)
	}
}

func TestParseSource_RejectsChangedSourceBeforeExtraction(t *testing.T) {
	data := []byte("invented")
	v := testPDFVersion("invented-pdf", data, 3)
	v.Files[0].SHA256 = strings.Repeat("0", 64)
	extractor := &recordingExtractor{}
	_, err := ParseSourceWithTable(context.Background(), v, data, extractor, inventedTable())
	require.Error(t, err)
	require.Zero(t, extractor.calls)
}

func testPDFVersion(name string, data []byte, count int) registry.Framework {
	return registry.Framework{Parser: name, ParseSource: "x.pdf", ExpectedControls: &count, Files: []registry.File{{Path: "x.pdf", Role: "text", Format: "pdf", Size: int64(len(data)), SHA256: digest(data)}}}
}

func TestParseSource_RejectsTamperedCatalog(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"a","title":"family","controls":[]}]}}`)
	count := 1
	v := registry.Framework{Parser: "nist-oscal-800-53", ExpectedControls: &count, Files: []registry.File{{Path: "x.json", Role: "catalog", Format: "json", Size: int64(len(data)), SHA256: strings.Repeat("0", 64)}}}
	_, err := ParseSource(context.Background(), v, data, nil)
	require.ErrorContains(t, err, "source integrity")
}

func TestParseSource_CallbackCannotChangeExpectedCount(t *testing.T) {
	data := []byte("invented")
	v := testPDFVersion("invented-pdf", data, 2)
	table := inventedTable()
	entry := table["invented-pdf"]
	entry.Parse = func(_ parse.PDFInputs, _ string, expected *int) (*parse.Result, error) {
		*expected = 3
		return &parse.Result{Nodes: []parse.Node{{}, {}, {}}}, nil
	}
	table["invented-pdf"] = entry
	_, err := ParseSourceWithTable(context.Background(), v, data, &recordingExtractor{}, table)
	require.Error(t, err)
	require.Equal(t, 2, *v.ExpectedControls)
}

type mutatingExtractor struct{ table parse.Table }

func (m *mutatingExtractor) Extract(_ context.Context, _ []byte, _, _ bool) (parse.PDFInputs, error) {
	entry := m.table["invented-pdf"]
	entry.Parse = func(parse.PDFInputs, string, *int) (*parse.Result, error) { return nil, errors.New("changed parser") }
	m.table["invented-pdf"] = entry
	return parse.PDFInputs{Layout: []string{"invented"}}, nil
}

func TestParseSource_FreezesParserDuringExtraction(t *testing.T) {
	table := inventedTable()
	v := testPDFVersion("invented-pdf", []byte("invented"), 3)
	result, err := ParseSourceWithTable(context.Background(), v, []byte("invented"), &mutatingExtractor{table}, table)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 3)
}

func TestLazyPDFRuntime_CancelledSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runtime := NewLazyPDFRuntime(ctx)
	_, err := runtime.Extract(ctx, []byte("invented"), true, false)
	require.ErrorIs(t, err, context.Canceled)
}
