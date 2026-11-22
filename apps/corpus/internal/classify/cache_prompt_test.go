package classify

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

func TestDocumentPromptCachesStablePrefix(t *testing.T) {
	r, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	a, err := BuildRequest(r, Document{Key: "one", Title: "First"}, families())
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildRequest(r, Document{Key: "two", Title: "Second"}, families())
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(a.System)
	y, _ := json.Marshal(b.System)
	if string(x) != string(y) || !strings.Contains(string(x), `"cache_control":{"type":"ephemeral"}`) {
		t.Fatalf("stable prefix mismatch or no marker: %s / %s", x, y)
	}
	if strings.Count(string(x), "cache_control") != 1 {
		t.Fatalf("wrong marker count: %s", x)
	}
}

func TestArticlePromptCachesStablePrefix(t *testing.T) {
	r := articleRecipe(t)
	req := ArticleRequest{DocumentKey: "vn:vbpl:1", Articles: articleInputs(1, "one"), Nodes: articleNodes()[1:4]}
	a, err := BuildArticleRequest(r, req)
	if err != nil {
		t.Fatal(err)
	}
	req.Articles = articleInputs(1, "two")
	b, err := BuildArticleRequest(r, req)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(a.System)
	y, _ := json.Marshal(b.System)
	if string(x) != string(y) || !strings.Contains(string(x), `"cache_control":{"type":"ephemeral"}`) {
		t.Fatalf("stable prefix mismatch or no marker: %s / %s", x, y)
	}
	if strings.Count(string(x), "cache_control") != 1 {
		t.Fatalf("wrong marker count: %s", x)
	}
}

func TestDocumentPromptVersionChangesRequestKey(t *testing.T) {
	r, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	d := Document{Key: "one", Title: "First"}
	current, err := BuildRequest(r, d, families())
	if err != nil {
		t.Fatal(err)
	}
	r.PromptVersion = 2
	old, err := BuildRequest(r, d, families())
	if err != nil {
		t.Fatal(err)
	}
	a, err := requestKey(&batchapi.Request{Params: current})
	if err != nil {
		t.Fatal(err)
	}
	b, err := requestKey(&batchapi.Request{Params: old})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different prompt versions produced the same request key")
	}
}

func TestArticlePromptVersionChangesRequestKey(t *testing.T) {
	r := articleRecipe(t)
	req := ArticleRequest{DocumentKey: "vn:vbpl:1", Articles: articleInputs(1, "chapter"), Nodes: articleNodes()[1:4]}
	current, err := BuildArticleRequest(r, req)
	if err != nil {
		t.Fatal(err)
	}
	r.Article.PromptVersion = 1
	old, err := BuildArticleRequest(r, req)
	if err != nil {
		t.Fatal(err)
	}
	a, err := requestKey(&batchapi.Request{Params: current})
	if err != nil {
		t.Fatal(err)
	}
	b, err := requestKey(&batchapi.Request{Params: old})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different prompt versions produced the same request key")
	}
}

func TestCachePromptHasOnlyVariableDataInUserMessage(t *testing.T) {
	base, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := BuildRequest(base, Document{Key: "one", Title: "First"}, families())
	if err != nil {
		t.Fatal(err)
	}
	article := articleRecipe(t)
	art, err := BuildArticleRequest(article, ArticleRequest{DocumentKey: "vn:vbpl:1", Articles: articleInputs(1, "chapter"), Nodes: articleNodes()[1:4]})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []anthropic.MessageNewParams{doc, art} {
		if len(p.System) != 2 || len(p.Messages) != 1 {
			t.Fatalf("unexpected message shape: %+v", p)
		}
		first, _ := json.Marshal(p.System[0])
		last, _ := json.Marshal(p.System[1])
		message, _ := json.Marshal(p.Messages)
		if strings.Contains(string(first), "cache_control") || !strings.Contains(string(last), `"cache_control":{"type":"ephemeral"}`) {
			t.Fatalf("cache marker not on last stable block: %s %s", first, last)
		}
		if strings.Contains(string(message), "active_level_2_nodes") || strings.Contains(string(message), "allowed_subtrees") || strings.Contains(string(message), "untrusted data") {
			t.Fatalf("stable content in user message: %s", message)
		}
	}
}
