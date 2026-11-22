package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestOpenAIDocumentRequest_StableCount(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Article = nil
	r.Model = "gpt-6.2-sol"
	d := Document{Key: "key", Title: "title"}
	f := []Family{{Code: "1.1", Name: "name", ParentName: "parent"}}
	body, count, err := BuildOpenAIDocumentRequest(r, d, f)
	if err != nil {
		t.Fatal(err)
	}
	if digest(body) != "9983da110411c4e675b8b4f6ab840047516b9d8f9d18a38e5ecf39f99b9aeeb2" || digest(count) != "832e586c059e1a03c852c1c2fce04d99fe6c60cb1b278beeb55be7c8d7b75479" {
		t.Fatalf("document request bytes changed: %s %s", digest(body), digest(count))
	}
	body2, count2, err := BuildOpenAIDocumentRequest(r, d, f)
	if err != nil || !bytes.Equal(body, body2) || !bytes.Equal(count, count2) {
		t.Fatal("request not deterministic", err)
	}
	var b, c map[string]json.RawMessage
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(count, &c); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_output_tokens", "store", "prompt_cache_options"} {
		if _, ok := b[key]; !ok {
			t.Fatalf("missing %s", key)
		}
		delete(b, key)
	}
	if len(b) != len(c) {
		t.Fatalf("unexpected count keys: %s", count)
	}
	for k, v := range b {
		if !bytes.Equal(v, c[k]) {
			t.Fatalf("count differs on %s", k)
		}
	}
	for _, s := range []string{`"prompt_cache_breakpoint"`, `"ttl":"30m"`, `"strict":true`, `"truncation":"disabled"`, `"role":"developer"`, `"role":"user"`} {
		if !strings.Contains(string(body), s) {
			t.Fatalf("missing %s in %s", s, body)
		}
	}
	if strings.Count(string(body), `"prompt_cache_breakpoint"`) != 1 {
		t.Fatal("unexpected breakpoints")
	}
}

func TestOpenAIArticleRequest_SchemaAndCount(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Model = "gpt-6.2-astra"
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	req := ArticleRequest{DocumentKey: "key", Articles: articleInputs(2, ""), Nodes: articleNodes()}
	body, count, err := BuildOpenAIArticleRequest(r, req)
	if err != nil {
		t.Fatal(err)
	}
	if digest(body) != "216a90791c1ecfeb6c35fd366cdca6f9fe2c817fe742477bba3fb1a259e655f7" || digest(count) != "6a56e9a21034dc21687c86bd917d75ede4bba4bae789afc9e7dd03a120fc5af1" {
		t.Fatalf("article request bytes changed: %s %s", digest(body), digest(count))
	}
	var b, c map[string]json.RawMessage
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(count, &c); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"store", "prompt_cache_options", "max_output_tokens"} {
		delete(b, k)
	}
	for k, v := range b {
		if !bytes.Equal(v, c[k]) {
			t.Fatalf("count differs on %s", k)
		}
	}
	if len(b) != len(c) {
		t.Fatal("extra count fields")
	}
	for _, s := range []string{`"dieu-1"`, `"dieu-2"`, `"additionalProperties":false`, `"confidence"`, `allowed_subtrees`, `"prompt_cache_breakpoint"`} {
		if !strings.Contains(string(body), s) {
			t.Fatalf("missing %s", s)
		}
	}
}

func TestOpenAIRequest_RejectsUnsupported(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Article = nil
	r.Model = "gpt-6.2-sol"
	d := Document{Key: "key", Title: "title"}
	f := []Family{{Code: "1"}}
	for _, effort := range []string{"none", "minimal", "other", ""} {
		r.Effort = effort
		if _, _, err := BuildOpenAIDocumentRequest(r, d, f); err == nil {
			t.Fatalf("accepted %s", effort)
		}
	}
	r.Effort = "medium"
	r.MaxTokens = 0
	if _, _, err := BuildOpenAIDocumentRequest(r, d, f); err == nil {
		t.Fatal("accepted zero output")
	}
}

func TestOpenAIArticleRequest_RejectsSchemaLimit(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Model = "gpt-6.2-astra"
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	req := ArticleRequest{DocumentKey: "key", Articles: articleInputs(1251, ""), Nodes: articleNodes()}
	if _, _, err := BuildOpenAIArticleRequest(r, req); !errors.Is(err, ErrOpenAISchemaLimit) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAIRequest_AnthropicBytes(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = ""
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	for _, version := range []int{1, 2, 3} {
		base := r.documentRecipe()
		base.PromptVersion = version
		p, err := BuildRequest(base, Document{Key: "key", Title: "title"}, []Family{{Code: "1", Name: "name", ParentName: "parent"}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		want := map[int]string{1: "7123fb21684e49bd2cf2dd95bb27bcbccf4aa81d8391e698b267007955d8e598", 2: "7123fb21684e49bd2cf2dd95bb27bcbccf4aa81d8391e698b267007955d8e598", 3: "167892309beaa051f36fd07979aa5a540502e2952c07fa9bcc7357b8c12403f9"}[version]
		if digest(b) != want {
			t.Fatalf("document %d bytes changed", version)
		}
	}
	for _, version := range []int{1, 3, 5} {
		a := r
		a.Article = new(ArticleRecipe)
		*a.Article = *r.Article
		a.Article.PromptVersion = version
		req := ArticleRequest{DocumentKey: "key", Articles: articleInputs(1, ""), Nodes: articleNodes()}
		if version == 3 {
			a.Article.Step = 1
			req.Step = 1
			req.Nodes = []Node{{Code: "1.1", Level: 2, Active: true, Origin: "phapdien"}}
		}
		p, err := BuildArticleRequest(a, req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		want := map[int]string{1: "055be8a2d7f892ffde2aa6203c8737b22682375b7483242d70412aa1243f8aa2", 3: "f2a4efa56804563fcce737f08bb763bcf723380c6718cec340403dc4fa8d144c", 5: "b41cfad325c318a595bff7b4a9af8be7485798afd1ec42fb0993898c292f387c"}[version]
		if digest(b) != want {
			t.Fatalf("article %d bytes changed", version)
		}
	}
}

func TestOpenAIArticleRequest_RejectsInvalidAnchorUTF8(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Model = "gpt-6.2-astra"
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	req := ArticleRequest{DocumentKey: "key", Articles: []Article{{Anchor: string([]byte{0xff})}}, Nodes: articleNodes()}
	if _, _, err := BuildOpenAIArticleRequest(r, req); err == nil {
		t.Fatal("accepted invalid UTF-8 anchor")
	}
}

func TestOpenAIRequest_EffortMatrix(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max", "none", "minimal", "other"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				r := articleRecipe(t)
				r.Provider = "openai"
				r.Model = model
				r.Effort = effort
				r.Article.SourceRecipeHash = r.documentRecipe().Hash()
				_, _, docErr := BuildOpenAIDocumentRequest(r.documentRecipe(), Document{Key: "key", Title: "title"}, []Family{{Code: "1"}})
				_, _, articleErr := BuildOpenAIArticleRequest(r, ArticleRequest{DocumentKey: "key", Articles: articleInputs(1, ""), Nodes: articleNodes()})
				valid := effort == "low" || effort == "medium" || effort == "high" || effort == "xhigh" || effort == "max"
				if (docErr == nil) != valid || (articleErr == nil) != valid {
					t.Fatalf("document: %v, article: %v", docErr, articleErr)
				}
			})
		}
	}
}

func TestOpenAIRequest_OutputBounds(t *testing.T) {
	for _, n := range []int64{15, 16, 32000, 32001} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := articleRecipe(t)
			r.Provider = "openai"
			r.Model = "gpt-6.2-sol"
			r.MaxTokens = n
			r.Article.SourceRecipeHash = r.documentRecipe().Hash()
			r.Article.MaxTokens = n
			db, dc, de := BuildOpenAIDocumentRequest(r.documentRecipe(), Document{Key: "key", Title: "title"}, []Family{{Code: "1"}})
			ab, ac, ae := BuildOpenAIArticleRequest(r, ArticleRequest{DocumentKey: "key", Articles: articleInputs(1, ""), Nodes: articleNodes()})
			valid := n >= 16 && n <= 32000
			if (de == nil) != valid || (ae == nil) != valid {
				t.Fatalf("document: %v, article: %v", de, ae)
			}
			if !valid && (db != nil || dc != nil || ab != nil || ac != nil) {
				t.Fatal("invalid request returned bytes")
			}
		})
	}
}

func TestOpenAIDocumentRequest_RejectsArticleRecipe(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Model = "gpt-6.2-sol"
	r.Article.SourceRecipeHash = r.documentRecipe().Hash()
	b, c, err := BuildOpenAIDocumentRequest(r, Document{Key: "key", Title: "title"}, []Family{{Code: "1"}})
	if err == nil || b != nil || c != nil {
		t.Fatal("accepted article recipe")
	}
}

func TestOpenAIRequest_CacheTTL(t *testing.T) {
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Article = nil
	r.Model = "gpt-6.2-sol"
	body, count, err := BuildOpenAIDocumentRequest(r, Document{Key: "key", Title: "title"}, []Family{{Code: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	var b, c map[string]json.RawMessage
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(count, &c); err != nil {
		t.Fatal(err)
	}
	if string(b["prompt_cache_options"]) != `{"mode":"explicit","ttl":"30m"}` {
		t.Fatalf("cache options: %s", b["prompt_cache_options"])
	}
	if strings.Count(string(b["input"]), `"prompt_cache_breakpoint":{"mode":"explicit","ttl":"30m"}`) != 1 {
		t.Fatalf("breakpoints: %s", b["input"])
	}
	for _, k := range []string{"max_output_tokens", "store", "prompt_cache_options"} {
		delete(b, k)
		if _, ok := c[k]; ok {
			t.Fatalf("count contains %s", k)
		}
	}
	if len(b) != len(c) {
		t.Fatal("count field mismatch")
	}
	for k, v := range b {
		if !bytes.Equal(v, c[k]) {
			t.Fatalf("count differs on %s", k)
		}
	}
}
