package classify

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderLedger_RejectsOpenAIIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.json")
	raw, err := json.Marshal(Submission{Provider: "openai", Items: []Item{}, Families: []Family{}, Results: map[string]Outcome{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var submission Submission
	err = readRetainedFile(path, &submission)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

type providerGuardClient struct {
	*fakeClient
	cancelled bool
}

func (c *providerGuardClient) Cancel(context.Context, string) error {
	c.cancelled = true
	return nil
}

func TestProviderLedger_ScreenRejectsOpenAI(t *testing.T) {
	r, _, client, options := screenFixture(t)
	var err error
	options.Recipe, err = NewOpenAIRecipe("gpt-6.2-sol", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Screen(context.Background(), options, documents(), families()); err == nil {
		t.Fatal("screen accepted OpenAI recipe")
	}
	if client.counts+client.creates+client.gets+client.downloads != 0 {
		t.Fatal("screen accessed Anthropic client")
	}
}

func TestProviderLedger_CancelRejectsOpenAI(t *testing.T) {
	id := strings.Repeat("a", 32)
	deadline := time.Now().Add(time.Hour)
	ledger := &memoryLedger{rows: []Submission{{ID: id, Provider: "openai", State: "submitted", BatchID: "batch", DeadlineAt: &deadline}}}
	client := &providerGuardClient{fakeClient: &fakeClient{}}
	r := Runner{Repository: ledger, Client: client}
	if err := r.Cancel(context.Background(), id, t.TempDir()); err == nil || client.cancelled {
		t.Fatalf("cancel reached Anthropic client: %v", err)
	}
}

func TestProviderLedger_RecipeIdentity(t *testing.T) {
	anthropic, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil || anthropic.Provider != "anthropic" {
		t.Fatalf("Anthropic identity: %+v, %v", anthropic, err)
	}
	openai, err := NewOpenAIRecipe("gpt-6.2-sol", strings.Repeat("a", 64))
	if err != nil || openai.Provider != "openai" {
		t.Fatalf("OpenAI identity: %+v, %v", openai, err)
	}
	openai.Provider = ""
	if openai.Validate() == nil {
		t.Fatal("implicit OpenAI recipe accepted")
	}
}

func TestProviderLedger_ReserveUsesExactPriceVersion(t *testing.T) {
	r, err := NewOpenAIRecipe("gpt-6.2-sol", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.reserve(openAIBatchPriceVersion, 100)
	want, wantErr := openAIReserve(r.Model, openAIBatchPriceVersion, 100, r.InputBound, r.MaxTokens)
	if err != nil || wantErr != nil || got != want {
		t.Fatalf("reserve = %d, %v; want %d, %v", got, err, want, wantErr)
	}
	for _, version := range []string{"", pilotPriceVersion, batchPriceVersion} {
		if _, err := r.reserve(version, 100); err == nil {
			t.Fatalf("accepted wrong price version %q", version)
		}
	}
}

func TestProviderLedger_IntentIdentity(t *testing.T) {
	r, err := NewOpenAIRecipe("gpt-6.2-sol", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{Provider: "openai", ProjectID: "proj-a", Recipe: r, PriceVersion: openAIBatchPriceVersion, KeyVersion: keyVersion, Budget: "openai-trial", BudgetMicroUSD: 40000000}
	if _, err = submissionProvider(s); err != nil {
		t.Fatal(err)
	}
	s.PriceVersion = batchPriceVersion
	if _, err = submissionProvider(s); err == nil {
		t.Fatal("mismatched price accepted")
	}
}

func TestProviderLedger_BudgetTuple(t *testing.T) {
	for _, tc := range []struct {
		provider, project, budget string
		amount                    int64
		valid                     bool
	}{
		{"anthropic", "", "trial", 200000000, true},
		{"openai", "project", "openai-trial", 40000000, true},
		{"openai", "project", "openai-full", 175000000, true},
		{"openai", "", "openai-trial", 40000000, false},
		{"anthropic", "project", "trial", 200000000, false},
		{"openai", "project", "trial", 200000000, false},
	} {
		if got := validProviderBudget(tc.provider, tc.project, tc.budget, tc.amount); got != tc.valid {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}

func TestProviderLedger_OverflowAndIsolation(t *testing.T) {
	store := retainedStore{intents: []Submission{
		{ID: "a", Provider: "anthropic", ReserveMicroUSD: 200000000},
		{ID: "b", Provider: "openai", ReserveMicroUSD: 100},
	}}
	anthropic, err := store.ledgerCostFor("anthropic")
	if err != nil || anthropic != 200000000 {
		t.Fatalf("Anthropic ledger = %d: %v", anthropic, err)
	}
	openai, err := store.ledgerCostFor("openai")
	if err != nil || openai != 100 || openai >= 40000000 {
		t.Fatalf("OpenAI ledger = %d: %v", openai, err)
	}
	store.intents = append(store.intents, Submission{ID: "c", Provider: "openai", ReserveMicroUSD: math.MaxInt64})
	if _, err = store.ledgerCostFor("openai"); err == nil {
		t.Fatal("accepted overflowing ledger")
	}
}

func TestProviderLedger_IsolatesCosts(t *testing.T) {
	store := retainedStore{
		intents: []Submission{
			{ID: "a", Provider: "anthropic", ReserveMicroUSD: 200},
			{ID: "b", Provider: "openai", ReserveMicroUSD: 40},
			{ID: "c", Provider: "anthropic", ReserveMicroUSD: 90},
			{ID: "d", Provider: "openai", ReserveMicroUSD: 80},
		},
		settlements: map[string]settlementRecord{"c": {Cost: 5}, "d": {Cost: 7}},
	}
	for _, tc := range []struct {
		provider string
		want     int64
	}{{"anthropic", 205}, {"openai", 47}} {
		got, err := store.ledgerCostFor(tc.provider)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %d, %v; want %d", tc.provider, got, err, tc.want)
		}
	}
}
