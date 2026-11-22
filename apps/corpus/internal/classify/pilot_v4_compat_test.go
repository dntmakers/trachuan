package classify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotV4_LegacyItemsRoundTrip(t *testing.T) {
	raw := []byte(`[{"key":"synthetic","request_hash":"abc","counted_input_tokens":10,"article_request":{"document_key":"synthetic","document_title":"Title","document_codes":["A"],"prompt_version":4,"articles":[{"anchor":"1","heading":"H","text":"T"}],"allowed_subtrees":[{"code":"A","name":"Name","parent_code":"","level":1,"active":true,"origin":"test","has_provisions":true,"ordinal":2}],"counted_input_tokens":10}}]`)
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(compact.Bytes())
	want := hex.EncodeToString(hash[:])
	var items []Item
	if err := validateRetainedJSON(raw, &items); err != nil {
		t.Fatalf("retained decode: %v", err)
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	if got := itemsHash(items); got != want {
		t.Fatalf("historical hash %s, want %s", got, want)
	}
	var semantic any
	if err := json.Unmarshal(raw, &semantic); err != nil {
		t.Fatal(err)
	}
	db, err := json.Marshal(semantic)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip []Item
	if err = json.Unmarshal(db, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if got := itemsHash(roundtrip); got != want {
		t.Fatalf("database hash %s, want %s", got, want)
	}

	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	recipe, err := NewArticleRecipe(s.Recipe, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	recipe.Article.PromptVersion = 4
	s.Recipe = recipe
	s.KeyVersion, s.PriceVersion = 0, ""
	s.Items = items
	s.ItemsHash = want
	s.ReserveMicroUSD, err = s.Recipe.reserve(s.PriceVersion, s.Items[0].InputTokens)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateRecovery(s, s.ID); err != nil {
		t.Fatalf("historical intent rejected: %v", err)
	}
	intent, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "intent.json")
	if err = os.WriteFile(path, intent, 0600); err != nil {
		t.Fatal(err)
	}
	var retained Submission
	if err = readRetainedFile(path, &retained); err != nil {
		t.Fatalf("retained intent: %v", err)
	}
	if retained.ItemsHash != want || itemsHash(retained.Items) != want {
		t.Fatal("retained hash changed")
	}
	workshop := s
	workshop.Items = roundtrip
	store := retainedStore{byID: map[string]Submission{s.ID: retained}}
	if err = r.verifyLedgerRows([]Submission{workshop}, &store); err != nil {
		t.Fatalf("workshop row: %v", err)
	}
	s.Items[0].Article.DocumentKey = "changed"
	if validateRecovery(s, s.ID) == nil {
		t.Fatal("changed immutable item accepted")
	}
	s.Items[0].Article.DocumentKey = "synthetic"
	s.Recipe.Article.PromptVersion = 5
	legacy := *s.Items[0].Article
	for _, tc := range []struct {
		name string
		set  func(*ArticleRequest)
	}{
		{"title", func(a *ArticleRequest) { a.DocumentTitle = "Title" }},
		{"codes", func(a *ArticleRequest) { a.DocumentCodes = []string{"A"} }},
		{"prompt", func(a *ArticleRequest) { a.PromptVersion = 4 }},
		{"provisions", func(a *ArticleRequest) { a.Nodes[0].HasProvisions = true }},
		{"ordinal", func(a *ArticleRequest) { a.Nodes[0].Ordinal = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := legacy
			a.DocumentTitle, a.DocumentCodes, a.PromptVersion = "", nil, 0
			a.Nodes = append([]Node(nil), legacy.Nodes...)
			a.Nodes[0].HasProvisions, a.Nodes[0].Ordinal = false, 0
			tc.set(&a)
			s.Items[0].Article = &a
			s.ItemsHash = itemsHash(s.Items)
			if validateRecovery(s, s.ID) == nil {
				t.Fatal("current intent accepted legacy field")
			}
		})
	}
}

func TestPilotV4_CurrentHashUnchanged(t *testing.T) {
	raw := []byte(`[{"key":"synthetic","request_hash":"abc","counted_input_tokens":10,"article_request":{"document_key":"synthetic","articles":null,"allowed_subtrees":[{"code":"A","name":"Name","parent_code":"","level":1,"active":true,"origin":"test"}],"counted_input_tokens":10}}]`)
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if got, want := itemsHash(items), hex.EncodeToString(hash[:]); got != want {
		t.Fatalf("current hash %s, want %s", got, want)
	}
}
