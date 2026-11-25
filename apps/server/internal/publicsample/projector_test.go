package publicsample

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"

	"danny.vn/trachuan/internal/retrieval"
)

func sample() (retrieval.Response, retrieval.SearchResult) {
	id := retrieval.Identity{Country: "vn", DocumentKey: DocumentKey, Anchor: "art_9"}
	text := strings.Repeat("body text ", 100)
	h := retrieval.Hit{Identity: id, Text: text, Fragments: []retrieval.Fragment{{Identity: id, Role: "body", Start: 0, End: len([]rune(text)), TextStart: 0, TextEnd: len([]rune(text))}}}
	snippet, complete := retrieval.Snippet(h, retrieval.SnippetCap)
	return retrieval.Response{Hits: []retrieval.Hit{h}}, retrieval.SearchResult{Hits: []retrieval.PresentedHit{{Handle: retrieval.Handle(id), Doc: DocumentKey, Citation: "citation", Snippet: snippet, Complete: complete, Text: "SECRET"}}, Documents: map[string]retrieval.DocumentEntry{DocumentKey: {Number: DocumentNumber, TextSource: "source_tree", Title: "title", SourceURL: CanonicalURL}}}
}
func TestProject(t *testing.T) {
	raw, presented := sample()
	presented.Hits[0].Path = []retrieval.PresentedPathNode{{Heading: "SECRET_PATH"}}
	presented.Hits[0].Warnings = []retrieval.Warning{{Basis: "SECRET_WARNING"}}
	presented.Hits[0].Gaps = []retrieval.Gap{{Message: "SECRET_GAP"}}
	presented.Hits[0].Relations = []retrieval.Relation{{Evidence: "SECRET_RELATION"}}
	presented.Gaps = []retrieval.Gap{{Message: "SECRET_ROOT_GAP"}}
	presented.Hits = append(presented.Hits, retrieval.PresentedHit{Text: "SECRET_SECOND_HIT"})
	result, reason := Project(raw, presented, "release")
	if reason != "" {
		t.Fatal(reason)
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("leaked forbidden field")
	}
	validateSampleSchema(t, b)
	var v PublicSampleResult
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	if len(v.Hits) != 1 || v.Hits[0].Doc != DocumentKey || v.Release != "release" || v.Abstain || v.Hits[0].Complete {
		t.Fatal(string(b))
	}
}
func TestProjectRefusals(t *testing.T) {
	tests := []struct {
		name   string
		change func(*retrieval.Response, *retrieval.SearchResult, *string)
		reason Reason
	}{
		{"abstain", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { p.Abstain = true }, ReasonAbstain},
		{"no hit", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { p.Hits = nil }, ReasonNoHit},
		{"wrong doc", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { p.Hits[0].Doc = "bad" }, ReasonWrongDocument},
		{"wrong number", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) {
			d := p.Documents[DocumentKey]
			d.Number = "bad"
			p.Documents[DocumentKey] = d
		}, ReasonWrongNumber},
		{"no cut", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { r.Hits[0].Fragments = nil }, ReasonNoProvenCut},
		{"mismatch", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { p.Hits[0].Snippet = "wrong" }, ReasonMismatch},
		{"schema", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) { *s = "" }, ReasonSchema},
		{"invalid handle", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) {
			r.Hits[0].Identity.Anchor = "a\nb"
			for i := range r.Hits[0].Fragments {
				r.Hits[0].Fragments[i].Identity = r.Hits[0].Identity
			}
			p.Hits[0].Handle = retrieval.Handle(r.Hits[0].Identity)
		}, ReasonSchema},
		{"cap", func(r *retrieval.Response, p *retrieval.SearchResult, s *string) {
			d := p.Documents[DocumentKey]
			d.Title = strings.Repeat("x", 17000)
			p.Documents[DocumentKey] = d
		}, ReasonBodyCap},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, p := sample()
			release := "release"
			tt.change(&r, &p, &release)
			_, got := Project(r, p, release)
			if got != tt.reason {
				t.Fatalf("got %q want %q", got, tt.reason)
			}
		})
	}
}
func validateSampleSchema(t *testing.T, body []byte) {
	t.Helper()
	data, err := os.ReadFile("../../../../packages/schema/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err = yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	defs := map[string]any{}
	for _, name := range []string{"PublicSampleResult", "PublicSampleHit", "PublicSampleDocument", "PublicSampleStatus"} {
		defs[name] = doc.Components.Schemas[name]
	}
	schemaJSON, err := json.Marshal(map[string]any{"$ref": "#/$defs/PublicSampleResult", "$defs": defs})
	if err != nil {
		t.Fatal(err)
	}
	schemaJSON = []byte(strings.ReplaceAll(string(schemaJSON), "#/components/schemas/", "#/$defs/"))
	var schema jsonschema.Schema
	if err = json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var instance any
	if err = json.Unmarshal(body, &instance); err != nil {
		t.Fatal(err)
	}
	if err = resolved.Validate(instance); err != nil {
		t.Fatal(err)
	}
	root := instance.(map[string]any)
	docs := root["documents"].(map[string]any)
	document := docs[DocumentKey].(map[string]any)
	for _, object := range []map[string]any{root, root["hits"].([]any)[0].(map[string]any), docs, document, document["status"].(map[string]any)} {
		object["forbidden"] = "secret"
		if resolved.Validate(root) == nil {
			t.Fatal("schema accepted extra property")
		}
		delete(object, "forbidden")
	}
}

func TestQueryFixture(t *testing.T) {
	b, err := os.ReadFile("../../../corpus/internal/evaluation/testdata/vn/legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	}
	if err = json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.ID == "pdpl-consent-91-2025" {
			if r.Question != Query {
				t.Fatal("query differs from fixture")
			}
			return
		}
	}
	t.Fatal("fixture missing")
}
