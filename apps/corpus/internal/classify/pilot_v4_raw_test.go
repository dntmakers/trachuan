package classify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotV4_RetainedRejectsLegacyPresence(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	recipe, err := NewArticleRecipe(s.Recipe, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	s.Recipe = recipe
	s.Items[0].Article = &ArticleRequest{DocumentKey: "synthetic", Articles: []Article{}, Nodes: []Node{{Code: "A"}}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baseline, b, 0600); err != nil {
		t.Fatal(err)
	}
	var original Submission
	if err := readRetainedFile(baseline, &original); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"title", `"document_key":"synthetic"`, `"document_key":"synthetic","document_title":""`},
		{"codes", `"document_key":"synthetic"`, `"document_key":"synthetic","document_codes":[]`},
		{"prompt", `"document_key":"synthetic"`, `"document_key":"synthetic","prompt_version":0`},
		{"provisions", `"origin":""`, `"origin":"","has_provisions":false`},
		{"ordinal", `"origin":""`, `"origin":"","ordinal":0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(string(b), tc.old, tc.replacement, 1)
			if raw == string(b) {
				t.Fatal("fixture replacement failed")
			}
			path := filepath.Join(t.TempDir(), "intent.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			var got Submission
			if err := readRetainedFile(path, &got); err == nil {
				t.Fatal("accepted explicit legacy field")
			}
		})
	}
}

func TestPilotV4_CurrentVersionsRejectHistoricalPrompt(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	recipe, err := NewArticleRecipe(s.Recipe, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	recipe.Article.PromptVersion = 4
	s.Recipe = recipe
	s.ReserveMicroUSD, err = s.Recipe.reserve(s.PriceVersion, s.Items[0].InputTokens)
	if err != nil {
		t.Fatal(err)
	}
	if validateRecovery(s, s.ID) == nil {
		t.Fatal("accepted prompt 4 with current versions")
	}
	for _, tc := range []struct {
		key     int
		price   string
		present bool
		valid   bool
	}{
		{0, "", false, true},
		{0, "", true, false},
		{0, batchPriceVersion, true, false},
		{keyVersion, "", false, false},
		{keyVersion, batchPriceVersion, true, false},
	} {
		s.KeyVersion, s.PriceVersion = tc.key, tc.price
		if err := validateLegacyItems([]byte(`[]`), s, tc.present); (err == nil) != tc.valid {
			t.Errorf("key=%d price=%q present=%t: %v", tc.key, tc.price, tc.present, err)
		}
	}
}
