package releasecheck

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/releasechanges"
)

const reportFixture = "../../../../deploy/host/updater/fixtures/release-check-report.json"

func fixtureProjection() (releasechanges.Header, releasechanges.Row) {
	previous, identity, key := "20990101", strings.Repeat("a", 64), "vn:vbpl:1"
	header := releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: "20990102", Predecessor: &previous, PredecessorIdentityHash: &identity}
	return header, releasechanges.Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"cause":"membership","key":"vn:vbpl:1"}`}
}

func encodeReport(t *testing.T, projection *ProjectionReport) []byte {
	t.Helper()
	recipe := embedding.QueryRecipe1()
	running := embedding.Report{ProtocolVersion: 1, RecipeHash: recipe.Hash(), Recipe: recipe}
	client := embedding.NewClient("http://embedder", &http.Client{Transport: recipeTransport{running}})
	report, err := Describe(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	report.ChangesProjection = projection
	var out bytes.Buffer
	if err = json.NewEncoder(&out).Encode(report); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func readReportFixture(t *testing.T) []byte {
	t.Helper()
	want, err := os.ReadFile(filepath.FromSlash(reportFixture))
	if err != nil {
		t.Fatal(err)
	}
	return want
}

func TestReport_FixtureEqualsGoOutput(t *testing.T) {
	header, row := fixtureProjection()
	hasher, err := releasechanges.NewHasher(header)
	if err != nil {
		t.Fatal(err)
	}
	if err = hasher.Add(row); err != nil {
		t.Fatal(err)
	}
	projection := &ProjectionReport{Version: releasechanges.Version, SHA256: hasher.Sum(), Release: header.Release, Previous: header.Predecessor, Counts: hasher.Counts()}
	if got := encodeReport(t, projection); !bytes.Equal(got, readReportFixture(t)) {
		t.Fatalf("release-check fixture differs from Go output:\n%s", got)
	}
}
