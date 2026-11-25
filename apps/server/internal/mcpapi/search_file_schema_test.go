package mcpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchSchema_NoFileFields(t *testing.T) {
	h := newHarness(t, nil)
	tools, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "search" {
			continue
		}
		raw, err := json.Marshal(tool.OutputSchema)
		if err != nil || strings.Contains(string(raw), `"files"`) || strings.Contains(string(raw), `"files_omitted"`) {
			t.Fatalf("search schema includes files: %s, %v", raw, err)
		}
		return
	}
	t.Fatal("search tool missing")
}
