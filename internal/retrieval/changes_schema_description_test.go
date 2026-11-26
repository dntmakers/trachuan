package retrieval

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestChangeSchema_AllRulesReachValidator(t *testing.T) {
	schemas := ChangePayloadSchemas()
	if len(schemas) != 6 {
		t.Fatalf("kinds: %d", len(schemas))
	}
	for kind, schema := range schemas {
		t.Run(kind, func(t *testing.T) { verifyChangeSchema(t, schema) })
	}
}

func verifyChangeSchema(t *testing.T, schema ChangeSchema) {
	t.Helper()
	value := changeSchemaSample(schema)
	raw, err := json.Marshal(value)
	if err != nil || !validSchemaValue(raw, schema) {
		t.Fatalf("valid %s: %v", raw, err)
	}
	if schema.Nullable && !validSchemaValue(json.RawMessage(`null`), schema) {
		t.Fatal("nullable rejected")
	}
	if schema.Type == "string" {
		if validSchemaValue(json.RawMessage(`true`), schema) {
			t.Fatal("wrong type accepted")
		}
		if schema.Format == "date" && validSchemaValue(json.RawMessage(`"2024-02-30"`), schema) {
			t.Fatal("invalid date accepted")
		}
		if schema.Format == "document-key" && validSchemaValue(json.RawMessage(`"vn:a#b"`), schema) {
			t.Fatal("invalid key accepted")
		}
		if schema.MaxLength > 0 {
			long, _ := json.Marshal("vn:" + strings.Repeat("a", schema.MaxLength))
			if validSchemaValue(long, schema) {
				t.Fatal("oversized string accepted")
			}
		}
	}
	if len(schema.Enum) > 0 {
		if validSchemaValue(json.RawMessage(`"not-an-enum"`), schema) {
			t.Fatal("invalid enum accepted")
		}
		for _, v := range schema.Enum {
			encoded, _ := json.Marshal(v)
			if !validSchemaValue(encoded, schema) {
				t.Fatalf("enum %s rejected", v)
			}
		}
	}
	if schema.Type != "object" {
		return
	}
	object := value.(map[string]any)
	for name, field := range schema.Fields {
		t.Run(name, func(t *testing.T) {
			verifyChangeSchema(t, field.Schema)
			if field.Required {
				copy := make(map[string]any, len(object))
				for k, v := range object {
					if k != name {
						copy[k] = v
					}
				}
				encoded, _ := json.Marshal(copy)
				if validSchemaValue(encoded, schema) {
					t.Fatal("missing required field accepted")
				}
			}
		})
	}
	object["unexpected"] = true
	raw, _ = json.Marshal(object)
	if validSchemaValue(raw, schema) {
		t.Fatal("extra field accepted")
	}
}

func changeSchemaSample(schema ChangeSchema) any {
	switch schema.Type {
	case "object":
		value := make(map[string]any, len(schema.Fields))
		for name, field := range schema.Fields {
			value[name] = changeSchemaSample(field.Schema)
		}
		return value
	case "boolean":
		return true
	case "string":
		if len(schema.Enum) > 0 {
			return schema.Enum[0]
		}
		switch schema.Format {
		case "document-key":
			return "vn:key"
		case "date":
			return "2024-02-29"
		}
		return "value"
	default:
		panic(fmt.Sprintf("unknown schema type %q", schema.Type))
	}
}
