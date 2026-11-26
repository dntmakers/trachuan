package releasechange

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

type differentialBudget struct {
	used  int
	limit int
}

func (b *differentialBudget) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.used {
		return 0, fmt.Errorf("projection exceeds byte limit")
	}
	b.used += len(p)
	return io.Discard.Write(p)
}

func TestProjection_BuildBudgetDifferential(t *testing.T) {
	budget := &differentialBudget{limit: 128 * 1024 * 1024}
	writer, err := releasechanges.NewWriter(releasechanges.Header{Version: 1, Country: "vn", Release: "r"}, budget)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"text":"` + strings.Repeat("x", 3800) + `"}`
	for ordinal := int64(1); ; ordinal++ {
		key := fmt.Sprintf("k%06d", ordinal)
		row := releasechanges.Row{Ordinal: ordinal, Kind: "document_added", Key: &key, Payload: payload}
		if err = writer.Add(row); err != nil {
			break
		}
	}
	got := struct {
		Bytes  int    `json:"bytes"`
		Rows   int    `json:"rows"`
		SHA256 string `json:"sha256"`
	}{budget.used, writer.Counts()["document_added"], writer.Sum()}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/projection_budget_case.json")
	if err != nil || strings.TrimSpace(string(data)) != string(encoded) {
		t.Fatalf("expected budget case %s", encoded)
	}
}
