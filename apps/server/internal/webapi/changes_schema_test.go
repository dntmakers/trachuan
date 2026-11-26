package webapi

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/internal/retrieval"
	"gopkg.in/yaml.v3"
)

func TestChanges_OpenAPIPayloadSchemas(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/schema/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	result := doc.Components.Schemas["ChangesResult"]
	mcpRaw, err := json.Marshal(mcpapi.ChangesOutputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var mcpSchema map[string]any
	if err = json.Unmarshal(mcpRaw, &mcpSchema); err != nil {
		t.Fatal(err)
	}
	assertSchemaJSONEqual(t, "state rules", result["allOf"], mcpSchema["allOf"])
	mcpProps := mcpSchema["properties"].(map[string]any)
	mcpItems := mcpProps["items"].(map[string]any)["items"].(map[string]any)["oneOf"].([]any)
	var mcpText map[string]any
	for _, candidate := range mcpItems {
		v := candidate.(map[string]any)
		kind := v["properties"].(map[string]any)["kind"].(map[string]any)
		if reflect.DeepEqual(kind["enum"], []any{"provision_text"}) {
			mcpText = v
			break
		}
	}
	if mcpText == nil {
		t.Fatal("missing MCP provision_text")
	}
	assertSchemaJSONEqual(t, "text if", doc.Components.Schemas["ChangeProvisionText"]["if"], mcpText["if"])
	assertSchemaJSONEqual(t, "text then", doc.Components.Schemas["ChangeProvisionText"]["then"], mcpText["then"])
	properties, _ := result["properties"].(map[string]any)
	items, _ := properties["items"].(map[string]any)
	item, _ := items["items"].(map[string]any)
	variants, _ := item["oneOf"].([]any)
	discriminator, _ := item["discriminator"].(map[string]any)
	mapping, _ := discriminator["mapping"].(map[string]any)
	if discriminator["propertyName"] != "kind" || len(variants) != len(retrieval.ChangePayloadSchemas()) || len(mapping) != len(variants) {
		t.Fatal("changes item discriminator or variants differ")
	}
	for kind, schema := range retrieval.ChangePayloadSchemas() {
		name := "Change"
		for _, part := range strings.Split(kind, "_") {
			name += strings.ToUpper(part[:1]) + part[1:]
		}
		ref := "#/components/schemas/" + name
		if mapping[kind] != ref {
			t.Errorf("%s discriminator mapping", kind)
		}
		found := false
		for _, variant := range variants {
			if v, ok := variant.(map[string]any); ok && v["$ref"] == ref {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing oneOf variant", kind)
		}
		got, ok := doc.Components.Schemas[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		checkChangeSchema(t, name, got, schema, true)
		props, _ := got["properties"].(map[string]any)
		kindSchema, _ := props["kind"].(map[string]any)
		if !reflect.DeepEqual(kindSchema["enum"], []any{kind}) {
			t.Errorf("%s kind discriminator", name)
		}
	}
}

func assertSchemaJSONEqual(t *testing.T, name string, a, b any) {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(x) != string(y) {
		t.Errorf("%s: %s != %s", name, x, y)
	}
}

func checkChangeSchema(t *testing.T, path string, got map[string]any, want retrieval.ChangeSchema, item bool) {
	t.Helper()
	types := []string{want.Type}
	if want.Nullable {
		types = append(types, "null")
	}
	var actual []string
	switch v := got["type"].(type) {
	case string:
		actual = []string{v}
	case []any:
		for _, x := range v {
			actual = append(actual, x.(string))
		}
	}
	sort.Strings(actual)
	sort.Strings(types)
	if !reflect.DeepEqual(actual, types) {
		t.Errorf("%s type %v want %v", path, actual, types)
	}
	var enums []string
	if values, ok := got["enum"].([]any); ok {
		for _, v := range values {
			enums = append(enums, v.(string))
		}
	}
	sort.Strings(enums)
	expected := append([]string(nil), want.Enum...)
	sort.Strings(expected)
	if !reflect.DeepEqual(enums, expected) {
		t.Errorf("%s enum %v want %v", path, enums, expected)
	}
	if want.Type == "string" {
		minLength := want.MinLength
		format := want.Format
		if format == "document-key" {
			format = ""
			minLength = 4
			if got["pattern"] != "^vn:" {
				t.Errorf("%s document key pattern", path)
			}
		}
		if got["format"] != nil && got["format"] != format || format != "" && got["format"] != format {
			t.Errorf("%s format %v want %s", path, got["format"], format)
		}
		if minLength > 0 && got["minLength"] != minLength {
			t.Errorf("%s minLength %v want %d", path, got["minLength"], minLength)
		}
		if want.MaxLength > 0 && got["maxLength"] != want.MaxLength {
			t.Errorf("%s maxLength %v want %d", path, got["maxLength"], want.MaxLength)
		}
	}
	if want.Type != "object" {
		return
	}
	if !want.AdditionalProperties && got["additionalProperties"] != false {
		t.Errorf("%s is not closed", path)
	}
	properties, _ := got["properties"].(map[string]any)
	expectedFields := len(want.Fields)
	if item {
		expectedFields++
		if !strings.HasSuffix(path, "DocumentRemoved") {
			expectedFields++
			handle, ok := properties["handle"].(map[string]any)
			if !ok || handle["type"] != "string" || handle["minLength"] != 1 || handle["maxLength"] != retrieval.ReadHandleMax {
				t.Errorf("%s handle schema", path)
			}
		}
	}
	if len(properties) != expectedFields {
		t.Errorf("%s fields %d want %d", path, len(properties), expectedFields)
	}
	required := map[string]bool{}
	if v, ok := got["required"].([]any); ok {
		for _, x := range v {
			required[x.(string)] = true
		}
	}
	if item {
		if !required["kind"] {
			t.Errorf("%s missing kind", path)
		}
		delete(required, "kind")
	}
	for name, field := range want.Fields {
		child, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("%s missing %s", path, name)
			continue
		}
		if required[name] != field.Required {
			t.Errorf("%s.%s required mismatch", path, name)
		}
		delete(required, name)
		checkChangeSchema(t, path+"."+name, child, field.Schema, false)
	}
	if len(required) > 0 {
		t.Errorf("%s extra required %v", path, required)
	}
}
