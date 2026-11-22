package classify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type accountFixture struct {
	*providerFault
	account string
}

func (f *accountFixture) AccountFingerprint() string { return f.account }

func TestEvidence_RejectsUnprovenListings(t *testing.T) {
	for _, scenario := range []string{"different account", "missing pagination", "missing result"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			client.createErr = context.DeadlineExceeded
			fault := &providerFault{fakeClient: client}
			account := &accountFixture{providerFault: fault, account: strings.Repeat("a", 32)}
			r.Client = account
			fault.pages = func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
				if scenario == "missing pagination" {
					return visit([]anthropic.MessageBatch{}, `{"data":[]}`)
				}
				raw := fmt.Sprintf(`{"data":[{"id":"candidate","created_at":%q,"processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0}}],"has_more":false,"first_id":"candidate","last_id":"candidate"}`, ledger.rows[0].CreatedAt.Format(time.RFC3339))
				var page struct {
					Data []anthropic.MessageBatch `json:"data"`
				}
				if err := json.Unmarshal([]byte(raw), &page); err != nil {
					return err
				}
				return visit(page.Data, raw)
			}
			fault.stream = func(context.Context, string, func(batchapi.Result) error) error { return nil }
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain create")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			if scenario == "different account" {
				account.account = strings.Repeat("b", 32)
				fault.pages = func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
					return visit([]anthropic.MessageBatch{}, `{"data":[],"has_more":false}`)
				}
			}
			if err := r.Reconcile(ctx, s.ID, true); err == nil {
				t.Fatal("abandoned with insufficient evidence")
			}
			if ledger.rows[0].State == "abandoned" {
				t.Fatal("released submission")
			}
		})
	}
}

func TestRecovery_AcceptsHistoricalB1KeyVersion(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	s.Recipe.PromptVersion = 2
	s.AccountFingerprint = ""
	request, err := BuildRequest(s.Recipe, documents()[0], families())
	if err != nil {
		t.Fatal(err)
	}
	key, err := requestKey(&batchapi.Request{Params: request})
	if err != nil {
		t.Fatal(err)
	}
	s.Items[0].RequestHash = key
	s.ItemsHash = itemsHash(s.Items)
	o.Recipe = s.Recipe
	s.ScreenHash = screenHash(o)
	if s.KeyVersion != 1 {
		t.Fatalf("unexpected key version %d", s.KeyVersion)
	}
	r.IntentDir = filepath.Join(t.TempDir(), "intents")
	if err = r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	ledger.rows = []Submission{s}
	if _, err = r.verifyStore(ledger.rows, nil); err != nil {
		t.Fatalf("admission rejected historical B1 intent: %v", err)
	}
	ledger.rows = nil
	if err = r.Rebuild(context.Background()); err != nil {
		t.Fatalf("rebuild rejected historical B1 intent: %v", err)
	}
	if len(ledger.rows) != 1 || ledger.rows[0].Recipe.PromptVersion != 2 || ledger.rows[0].KeyVersion != 1 {
		t.Fatalf("rebuilt rows %+v", ledger.rows)
	}
	observed := s.CreatedAt.Add(25 * time.Hour)
	r.Now = func() time.Time { return observed }
	_ = r.Reconcile(context.Background(), s.ID, true)
	r.Now = func() time.Time { return observed.Add(25 * time.Hour) }
	if err = r.Reconcile(context.Background(), s.ID, true); err == nil || !strings.Contains(err.Error(), "listing account differs") {
		t.Fatalf("unfingerprinted intent accepted listing: %v", err)
	}
	if ledger.rows[0].State == "abandoned" {
		t.Fatal("unfingerprinted intent was abandoned")
	}
}

func TestEvidence_InvalidAbandonmentBlocksAdmissionWithoutLedgerChanges(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.writeAbandonment(&s, captureReference{}); err != nil {
		t.Fatal(err)
	}
	before := append([]Submission(nil), ledger.rows...)
	creates := client.creates
	o.RunID++
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("accepted invalid abandonment")
	}
	if client.creates != creates || !reflect.DeepEqual(ledger.rows, before) {
		t.Fatal("invalid evidence changed ledger or called provider")
	}
}

func TestEvidence_CandidateResultLinesMatchRequestCounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		total int64
		valid bool
	}{
		{"complete distinct", []string{"other-1", "other-2"}, 2, true},
		{"missing", []string{"other-1"}, 2, false},
		{"duplicate", []string{"other-1", "other-1"}, 2, false},
		{"invalid id", []string{"bad space"}, 1, false},
		{"wrong prefix", []string{strings.Repeat("a", 32) + "-000000"}, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, _ := screenFixture(t)
			owner := strings.Repeat("a", 32)
			capture, err := r.startCapture(captureStart{Owner: owner, BatchID: "candidate", AccountFingerprint: strings.Repeat("a", 32)})
			if err != nil {
				t.Fatal(err)
			}
			for i, id := range tc.lines {
				body := `{"type":"expired"}`
				idBytes, marshalErr := json.Marshal(id)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				wire := `{"custom_id":` + string(idBytes) + `,"result":` + body + `}`
				if err = r.writeCapture(filepath.Join(capture.dir, captureRecordName(i)), capturedResult{CustomID: id, Type: "expired", Body: body, Wire: wire}); err != nil {
					t.Fatal(err)
				}
			}
			if err = r.endCapture(capture, len(tc.lines), nil); err != nil {
				t.Fatal(err)
			}
			err = r.verifyAbandonmentCandidate(owner, "candidate", strings.Repeat("a", 32), tc.total)
			if (err == nil) != tc.valid {
				t.Fatalf("error %v, expected valid %t", err, tc.valid)
			}
		})
	}
}

func TestEvidence_PaginationRequiresForwardFinalPage(t *testing.T) {
	a := []anthropic.MessageBatch{{ID: "a", CreatedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), ProcessingStatus: "ended"}}
	b := []anthropic.MessageBatch{{ID: "b", CreatedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), ProcessingStatus: "ended"}}
	first := strings.Replace(fakeListingRaw(a), `"has_more":false`, `"has_more":true`, 1)
	last := fakeListingRaw(b)
	pager := listingPager{}
	if _, err := pager.accept(first); err != nil || pager.complete() {
		t.Fatalf("first page error %v", err)
	}
	if _, err := pager.accept(last); err != nil || !pager.complete() {
		t.Fatalf("last page error %v", err)
	}
	for _, bad := range []string{`{"data":[]}`, `{"data":[],"has_more":true}`, `{"data":[],"has_more":false,"has_more":true}`} {
		var p listingPager
		if _, err := p.accept(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	var repeated listingPager
	if _, err := repeated.accept(first); err != nil {
		t.Fatal(err)
	}
	if _, err := repeated.accept(first); err == nil {
		t.Fatal("accepted repeated cursor")
	}
}

func TestEvidence_FingerprintMatchesAPIKeyHash(t *testing.T) {
	key := "synthetic-api-key"
	t.Setenv("ANTHROPIC_API_KEY", key)
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(key))
	if got, want := client.AccountFingerprint(), hex.EncodeToString(hash[:16]); got != want {
		t.Fatalf("fingerprint %s want %s", got, want)
	}
}

func TestEvidence_CandidateRequiresMatchingOriginalWire(t *testing.T) {
	for _, wire := range []string{"", `{"custom_id":"other-1"}`, `{"custom_id":"other-2","result":{"type":"expired"}}`, `{"custom_id":"other-1","result":{"type":"canceled"}}`} {
		t.Run(fmt.Sprintf("%q", wire), func(t *testing.T) {
			r, _, _, _ := screenFixture(t)
			owner := strings.Repeat("a", 32)
			c, err := r.startCapture(captureStart{Owner: owner, BatchID: "candidate", AccountFingerprint: strings.Repeat("a", 32)})
			if err != nil {
				t.Fatal(err)
			}
			record := capturedResult{CustomID: "other-1", Type: "expired", Body: `{"type":"expired"}`, Wire: wire}
			if err = r.writeCapture(filepath.Join(c.dir, captureRecordName(0)), record); err != nil {
				t.Fatal(err)
			}
			if err = r.endCapture(c, 1, nil); err != nil {
				t.Fatal(err)
			}
			if err = r.verifyAbandonmentCandidate(owner, "candidate", strings.Repeat("a", 32), 1); err == nil {
				t.Fatal("accepted missing or mismatched provider wire")
			}
		})
	}
}

func TestEvidence_CandidateRejectsOtherAccountCapture(t *testing.T) {
	r, _, _, _ := screenFixture(t)
	owner := strings.Repeat("a", 32)
	c, err := r.startCapture(captureStart{Owner: owner, BatchID: "candidate", AccountFingerprint: strings.Repeat("b", 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.endCapture(c, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err = r.verifyAbandonmentCandidate(owner, "candidate", strings.Repeat("a", 32), 0); err == nil {
		t.Fatal("accepted results from another account")
	}
}

func TestEvidence_ReverificationRejectsSelfConsistentAlteredProofs(t *testing.T) {
	for _, scenario := range []string{"pagination", "request count", "account", "quiet start", "window end"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			client.createErr = context.DeadlineExceeded
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain create")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			if err := r.Reconcile(ctx, s.ID, true); err != nil {
				t.Fatal(err)
			}
			recordPath := filepath.Join(r.IntentDir, "submissions", s.ID+".abandoned")
			var record abandonmentRecord
			if err := readRetainedFile(recordPath, &record); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(r.IntentDir, filepath.FromSlash(record.Path))
			if scenario == "account" || scenario == "quiet start" || scenario == "window end" {
				path := filepath.Join(dir, "start.json")
				var start captureStart
				if err := readRetainedFile(path, &start); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "account":
					start.AccountFingerprint = strings.Repeat("b", 32)
				case "window end":
					before := s.CreatedAt.Add(24*time.Hour + 59*time.Minute)
					start.Before = &before
				default:
					early := s.CreatedAt.Add(24*time.Hour + time.Minute)
					start.Before, start.CapturedAt = &early, &early
				}
				b, err := json.Marshal(start)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				path := filepath.Join(dir, captureRecordName(0))
				var raw string
				if err := readRetainedFile(path, &raw); err != nil {
					t.Fatal(err)
				}
				var page map[string]any
				if err := json.Unmarshal([]byte(raw), &page); err != nil {
					t.Fatal(err)
				}
				if scenario == "pagination" {
					delete(page, "has_more")
				} else {
					page["data"].([]any)[0].(map[string]any)["request_counts"].(map[string]any)["succeeded"] = float64(1)
				}
				pageBytes, err := json.Marshal(page)
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(string(pageBytes))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			files, err := readRetainedDirectory(dir)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := captureProof(r.IntentDir, dir, files)
			if err != nil {
				t.Fatal(err)
			}
			record.captureReference = proof
			b, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(recordPath, b, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = r.verifyStore(ledger.rows, nil); err == nil {
				t.Fatal("accepted altered but rehashed evidence")
			}
		})
	}
}
