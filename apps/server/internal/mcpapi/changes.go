package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"

	"danny.vn/trachuan/internal/retrieval"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func encodeChangesResult(value any, schema *jsonschema.Resolved) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, retrieval.ErrUnavailable
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return nil, retrieval.ErrUnavailable
	}
	raw, err = json.Marshal(decoded)
	if err != nil || json.Unmarshal(raw, &decoded) != nil || schema.Validate(decoded) != nil {
		return nil, retrieval.ErrUnavailable
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(raw), Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
}

type changesEngine interface {
	Changes(context.Context, retrieval.ChangesRequest) (retrieval.ChangesResult, error)
}

func (cfg Config) changes(ctx context.Context, req retrieval.ChangesRequest) (retrieval.ChangesResult, error) {
	lease, err := cfg.resolve(ctx, req.Country, "")
	if err != nil {
		return retrieval.ChangesResult{}, err
	}
	defer lease.Done()
	engine, ok := lease.Engine.(changesEngine)
	if !ok {
		return retrieval.ChangesResult{}, retrieval.ErrUnavailable
	}
	return engine.Changes(ctx, req)
}
