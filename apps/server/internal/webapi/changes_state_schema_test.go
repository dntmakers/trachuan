package webapi

import (
	"encoding/json"
	"os"
	"testing"

	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"
)

func TestChangesSchemas_StateAndHandleRules(t *testing.T) {
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
	output := doc.Components.Schemas["ChangesResult"]
	props := output["properties"].(map[string]any)
	items := props["items"].(map[string]any)["items"].(map[string]any)
	for i, v := range items["oneOf"].([]any) {
		ref := v.(map[string]any)["$ref"].(string)
		items["oneOf"].([]any)[i] = doc.Components.Schemas[ref[len("#/components/schemas/"):]]
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var openapi jsonschema.Schema
	if err = json.Unmarshal(data, &openapi); err != nil {
		t.Fatal(err)
	}
	web, err := openapi.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := mcpapi.ChangesOutputSchema().Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	counts := `"counts":{"document_added":0,"document_removed":0,"document_validity":0,"provision_validity":0,"provision_text":1,"document_key":0}`
	available := `{"country":"vn","release":"20991110","state":"available","previous_release":"20991109",` + counts + `,"items":`
	base := `{"country":"vn","release":"20991110","state":"baseline","items":[]`
	missing := `{"country":"vn","release":"20991110","state":"unavailable","items":[]`
	cases := []struct {
		data  string
		valid bool
	}{
		{available + `[]}`, true}, {`{"country":"vn","release":"20991110","state":"available","items":[]}`, false},
		{`{"country":"vn","release":"20991110","state":"available","previous_release":"20991109","items":[]}`, false},
		{`{"country":"vn","release":"20991110","state":"available",` + counts + `,"items":[]}`, false},
		{base + `}`, true}, {base + `,"previous_release":"20991109"}`, true}, {base + `,"previous_release":""}`, false}, {base + `,` + counts + `}`, false}, {base + `,"next_cursor":"x"}`, false},
		{missing + `}`, true}, {missing + `,"previous_release":"20991109"}`, false}, {missing + `,` + counts + `}`, false}, {missing + `,"next_cursor":"x"}`, false},
		{`{"country":"vn","release":"20991110","state":"baseline","items":[{"kind":"document_removed","key":"vn:1","cause":"source"}]}`, false},
		{`{"country":"vn","release":"20991110","state":"unavailable","items":[{"kind":"document_removed","key":"vn:1","cause":"source"}]}`, false},
		{available + `[{"kind":"provision_text","key":"vn:1","anchor":"p1","change":"removed","provenance":false,"ambiguous":false,"handle":"vn:1#p1"}]}`, false},
		{available + `[{"kind":"provision_text","key":"vn:1","anchor":"p1","change":"changed","provenance":false,"ambiguous":true,"handle":"vn:1#p1"}]}`, false},
		{available + `[{"kind":"provision_text","key":"vn:1","anchor":"p1","change":"changed","provenance":false,"ambiguous":false,"handle":"vn:1#p1"}]}`, true},
	}
	for _, tc := range cases {
		var v any
		if err = json.Unmarshal([]byte(tc.data), &v); err != nil {
			t.Fatal(err)
		}
		for name, s := range map[string]*jsonschema.Resolved{"web": web, "mcp": mcp} {
			err = s.Validate(v)
			if (err == nil) != tc.valid {
				t.Errorf("%s valid=%v err=%v: %s", name, tc.valid, err, tc.data)
			}
		}
	}
}
