package mcpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReadFiles_NullKeysInBothContents(t *testing.T) {
	result := retrieval.ReadResult{Release: "20991110_1", Items: []retrieval.ItemResult{{Doc: "vn:1", Document: &retrieval.ReadDocumentEntry{Files: []retrieval.SourceFile{{Name: "", Hash: strings.Repeat("a", 64), Role: "main"}}}}}}
	response, _, err := encodeResult(result, false)
	if err != nil {
		t.Fatal(err)
	}
	raw := response.StructuredContent.(json.RawMessage)
	text := response.Content[0].(*mcp.TextContent).Text
	if string(raw) != text || !strings.Contains(text, `"name":""`) || !strings.Contains(text, `"url":null`) || !strings.Contains(text, `"fetched_at":null`) {
		t.Fatalf("structured %s, text %s", raw, text)
	}
}
