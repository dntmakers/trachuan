package classify

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurableLedger_ChangedWorkshopFieldsBlockCreate(t *testing.T) {
	for _, tc := range []struct {
		field   string
		settled bool
		change  func(*Submission)
	}{
		{"reserve", false, func(s *Submission) { s.ReserveMicroUSD = 0 }},
		{"cost", true, func(s *Submission) { s.CostMicroUSD = 0 }},
		{"cost", false, func(s *Submission) { s.CostMicroUSD = 1 }},
		{"settled", true, func(s *Submission) { s.Settled = false }},
		{"budget name", false, func(s *Submission) { s.Budget = "trial" }},
		{"budget amount", false, func(s *Submission) { s.BudgetMicroUSD++ }},
		{"recipe hash", false, func(s *Submission) { s.Recipe.Effort = "high" }},
		{"item list hash", false, func(s *Submission) { s.ItemsHash = strings.Repeat("b", 64) }},
		{"key version", false, func(s *Submission) { s.KeyVersion = 0 }},
		{"price version", false, func(s *Submission) { s.PriceVersion = "another-price" }},
		{"batch receipt", false, func(s *Submission) { s.BatchID = "batch_unrelated" }},
	} {
		t.Run(fmt.Sprint(tc.field, "_", tc.settled), func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			f.ended = tc.settled
			if _, err := r.Screen(ctx, o, documents(), families()); (err == nil) != tc.settled || ledger.rows[0].Settled != tc.settled {
				t.Fatalf("unexpected fixture settlement: %v", err)
			}
			tc.change(&ledger.rows[0])
			f.beforeCreate = nil
			o.RunID++
			o.Out = filepath.Join(t.TempDir(), "unrelated")
			if _, err := r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Unrelated document"}}, families()); err == nil || !strings.Contains(err.Error(), tc.field) || f.creates != 1 {
				t.Fatalf("changed workshop %s did not stop Create: creates=%d, error=%v", tc.field, f.creates, err)
			}
		})
	}
}

func TestDurableLedger_PendingSettlementUsesDiskCostWithoutUpdatingDryRunRow(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	cost := ledger.rows[0].CostMicroUSD
	ledger.rows[0].State, ledger.rows[0].Settled, ledger.rows[0].CostMicroUSD = "submitted", false, 0
	r.Client = nil
	o.DryRun = true
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "preview")
	res, err := r.Screen(ctx, o, documents(), families())
	if err != nil || f.creates != 1 || res.Requests != 0 || res.LedgerMicroUSD != cost || ledger.rows[0].Settled {
		t.Fatalf("dry run did not use the durable settlement: %+v, %v", res, err)
	}
	if err = r.Rebuild(ctx); err != nil || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != cost {
		t.Fatalf("rebuild did not advance pending settlement: %v", err)
	}
}

func TestDurableLedger_PendingReceiptDoesNotBlockAdmissionOrRebuild(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	ledger.rows[0].BatchID, ledger.rows[0].SubmittedAt, ledger.rows[0].DeadlineAt = "", nil, nil
	ledger.rows[0].State = "preparing"
	o.DryRun = true
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "preview")
	r.Client = nil
	res, err := r.Screen(ctx, o, documents(), families())
	if err != nil || res.Requests != 0 || res.LedgerMicroUSD != s.ReserveMicroUSD || f.creates != 1 || ledger.rows[0].BatchID != "" {
		t.Fatalf("durable receipt was not usable during lag: %+v, %v", res, err)
	}
	if err = r.Rebuild(ctx); err != nil || ledger.rows[0].BatchID != s.BatchID || ledger.rows[0].State != "submitted" {
		t.Fatalf("rebuild did not attach durable receipt: %v", err)
	}
}

func TestDurableLedger_PilotImportRequiredBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	seedHistoricalPilot(t, r, ledger, o, 17)
	f.beforeCreate = nil
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "preview")
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || !strings.Contains(err.Error(), "pilot") || f.creates != 0 {
		t.Fatalf("admission used an unimported pilot cost: creates=%d, error=%v", f.creates, err)
	}
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	o.DryRun = true
	res, err := r.Screen(ctx, o, documents(), families())
	if err != nil || res.LedgerMicroUSD != 17 {
		t.Fatalf("admission did not use imported pilot cost: %+v, %v", res, err)
	}
}

func seedHistoricalPilot(t *testing.T, r *Runner, ledger *memoryLedger, o Options, paid int64) {
	t.Helper()
	recipe := o.Recipe
	if recipe.Article != nil {
		recipe = recipe.documentRecipe()
	}
	reserve, err := recipe.reserve("", 100)
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{
		ID: strings.Repeat("b", 32), RunID: o.RunID, Country: o.Country, Release: o.Release,
		Recipe: recipe, State: "preparing", CreatedAt: r.Now(), Budget: o.Budget, BudgetMicroUSD: o.BudgetMicroUSD,
		ReserveMicroUSD: reserve, Items: []Item{{Key: documents()[0].Key, InputTokens: 100}},
	}
	s.ItemsHash = itemsHash(s.Items)
	if err = r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	if err = r.recordBatch(&s, "batch_pilot"); err != nil {
		t.Fatal(err)
	}
	s.State, s.CostMicroUSD, s.Settled = "completed", paid, true
	ledger.rows = append(ledger.rows, s)
}

func TestDurableLedger_HistoryUsesRetainedCounts(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	retained, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := retained.intents[0].Items[0]
	ledger.rows[0].Items[0].InputTokens = 1
	h := indexingHistory(ledger.rows, retained)
	if h.counts[item.RequestHash] != item.InputTokens {
		t.Fatalf("workshop count replaced durable input count: %d", h.counts[item.RequestHash])
	}
}
