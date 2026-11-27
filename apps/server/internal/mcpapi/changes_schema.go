package mcpapi

import (
	"sort"

	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/retrieval"
	"github.com/google/jsonschema-go/jsonschema"
)

func configureChangesInput(s *jsonschema.Schema) {
	for _, name := range []string{"release", "document", "cursor"} {
		p := s.Properties[name]
		p.Types = nil
		p.Type = "string"
		p.MinLength = ptrInt(1)
	}
	s.Properties["release"].Pattern = `^[0-9]{8}(_[1-9][0-9]*)?$`
	s.Properties["release"].MaxLength = ptrInt(11)
	s.Properties["document"].Pattern = `^vn:`
	s.Properties["document"].MinLength = ptrInt(4)
	s.Properties["document"].MaxLength = ptrInt(retrieval.ReadHandleMax)
	s.Properties["cursor"].MaxLength = ptrInt(512)
	kinds := s.Properties["kinds"]
	kinds.Types = nil
	kinds.Type = "array"
	kinds.MinItems = ptrInt(1)
	kinds.UniqueItems = true
	kinds.Items.Enum = make([]any, 0, releasechanges.KindCount)
	for _, kind := range releasechanges.Kinds() {
		kinds.Items.Enum = append(kinds.Items.Enum, kind)
	}
	limit := s.Properties["limit"]
	limit.Types = nil
	limit.Type = "integer"
	limit.Minimum = ptrFloat(1)
	limit.Maximum = ptrFloat(50)
}

func changesPayloadSchema(source retrieval.ChangeSchema) *jsonschema.Schema {
	s := &jsonschema.Schema{Type: source.Type, Format: source.Format}
	if source.Nullable {
		s.Type = ""
		s.Types = []string{source.Type, "null"}
	}
	if source.MinLength > 0 {
		s.MinLength = ptrInt(source.MinLength)
	}
	if source.MaxLength > 0 {
		s.MaxLength = ptrInt(source.MaxLength)
	}
	if source.Format == "document-key" {
		s.Format = ""
		s.Pattern = `^vn:`
		s.MinLength = ptrInt(4)
	}
	for _, value := range source.Enum {
		s.Enum = append(s.Enum, value)
	}
	if source.Type == "object" {
		s.Properties = map[string]*jsonschema.Schema{}
		if !source.AdditionalProperties {
			s.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
		}
		for name, field := range source.Fields {
			s.Properties[name] = changesPayloadSchema(field.Schema)
			if field.Required {
				s.Required = append(s.Required, name)
			}
		}
		sort.Strings(s.Required)
	}
	return s
}

func ChangesOutputSchema() *jsonschema.Schema { return changesOutputSchema() }

func changesOutputSchema() *jsonschema.Schema {
	kinds := retrieval.ChangePayloadSchemas()
	names := make([]string, 0, len(kinds))
	for name := range kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	variants := make([]*jsonschema.Schema, 0, len(names))
	counts := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
	for _, name := range names {
		item := changesPayloadSchema(kinds[name])
		item.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{name}}
		item.Required = append(item.Required, "kind")
		if name != "document_removed" {
			item.Properties["handle"] = &jsonschema.Schema{Type: "string", MinLength: ptrInt(1), MaxLength: ptrInt(retrieval.ReadHandleMax)}
		}
		if name == "provision_text" {
			item.If = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
				{Properties: map[string]*jsonschema.Schema{"change": {Enum: []any{"removed"}}}, Required: []string{"change"}},
				{Properties: map[string]*jsonschema.Schema{"ambiguous": {Enum: []any{true}}}, Required: []string{"ambiguous"}},
			}}
			item.Then = &jsonschema.Schema{Not: &jsonschema.Schema{Required: []string{"handle"}}}
		}
		variants = append(variants, item)
		counts.Properties[name] = &jsonschema.Schema{Type: "integer", Minimum: ptrFloat(0)}
		counts.Required = append(counts.Required, name)
	}
	return &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"country":          {Type: "string", Enum: []any{"vn"}},
		"release":          {Type: "string"},
		"state":            {Type: "string", Enum: []any{"available", "baseline", "unavailable"}},
		"previous_release": {Type: "string", MinLength: ptrInt(1)},
		"counts":           counts,
		"items":            {Type: "array", Items: &jsonschema.Schema{OneOf: variants}},
		"next_cursor":      {Type: "string"},
	}, Required: []string{"country", "release", "state", "items"}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}, AllOf: []*jsonschema.Schema{
		{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Enum: []any{"available"}}}, Required: []string{"state"}}, Then: &jsonschema.Schema{Required: []string{"previous_release", "counts"}}},
		{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Enum: []any{"baseline", "unavailable"}}}, Required: []string{"state"}}, Then: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"items": {MaxItems: ptrInt(0)}}, Not: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Required: []string{"counts"}}, {Required: []string{"next_cursor"}}}}}},
		{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Enum: []any{"unavailable"}}}, Required: []string{"state"}}, Then: &jsonschema.Schema{Not: &jsonschema.Schema{Required: []string{"previous_release"}}}},
	}}
}
