package classify

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func TestVerifiedSettlement_AttemptsAloneDoNotReleaseReserve(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	a, _, err := parseAttempt(s, 0, `{"type":"expired"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.writeAttempt(s, 0, a); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err = r.Rebuild(ctx); err != nil || len(ledger.rows) != 1 || ledger.rows[0].Settled || ledger.rows[0].ReserveMicroUSD != s.ReserveMicroUSD {
		t.Fatalf("attempt without a complete capture freed reserve: %v", err)
	}
	if _, err = os.Stat(r.settlementPath(s)); !os.IsNotExist(err) {
		t.Fatalf("unexpected settlement file: %v", err)
	}
}

func TestVerifiedSettlement_AdmissionAndRebuildRejectMissingProof(t *testing.T) {
	for _, lost := range []string{"settlement", "attempt", "capture"} {
		t.Run(lost, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
				t.Fatal(err)
			}
			s := ledger.rows[0]
			var path string
			switch lost {
			case "settlement":
				path = r.settlementPath(s)
			case "attempt":
				path = r.attemptPath(s, 0)
			case "capture":
				if err := filepath.WalkDir(filepath.Join(r.IntentDir, "evidence"), func(name string, f fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if f.Name() == "start.json" {
						path = filepath.Dir(name)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			if err := r.Rebuild(ctx); err == nil {
				t.Error("rebuild accepted missing " + lost)
			}
			o.RunID++
			o.Out = filepath.Join(t.TempDir(), "unrelated")
			f.beforeCreate = nil
			if _, err := r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Another document"}}, families()); err == nil || f.creates != 1 {
				t.Fatalf("admission ignored missing %s: creates=%d, error %v", lost, f.creates, err)
			}
		})
	}
}

func TestVerifiedSettlement_CompleteCaptureIsReferencedAndHashed(t *testing.T) {
	ctx := context.Background()
	r, ledger, _, o := screenFixture(t)
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	var proof struct {
		CapturePath string `json:"capture_path"`
		CaptureHash string `json:"capture_sha256"`
	}
	if err := readRetainedFile(r.settlementPath(ledger.rows[0]), &proof); err != nil || proof.CapturePath == "" || !validKey(proof.CaptureHash) {
		t.Fatalf("settlement omitted capture proof: %+v, %v", proof, err)
	}
	line := filepath.Join(r.IntentDir, proof.CapturePath, captureRecordName(0))
	body, err := os.ReadFile(line)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	if err = json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	original["extra"] = json.RawMessage(`"tampered"`)
	body, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(line, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.Rebuild(ctx); err == nil {
		t.Fatal("rebuild ignored capture hash mismatch")
	}
}

func TestVerifiedSettlement_ForeignOnlyCapturePoisonsReceiptedBatch(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	proper := f.results
	f.results = []batchapi.Result{{CustomID: strings.Repeat("e", 32) + "-000000", Type: "expired"}}
	if err := r.captureResults(ctx, s.ID, s.BatchID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	f.results = proper
	if err := r.captureResults(ctx, s.ID, s.BatchID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Rebuild(ctx); err == nil || !strings.Contains(err.Error(), "foreign") || ledger.rows[0].Settled {
		t.Fatalf("verified batch ignored a wholly foreign capture: %v", err)
	}
}

func TestVerifiedSettlement_UnsettledAttemptMustAgreeWithCapture(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	if err := r.captureResults(ctx, s.ID, s.BatchID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	a, _, err := parseAttempt(s, 0, `{"type":"expired"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.writeAttempt(s, 0, a); err != nil {
		t.Fatal(err)
	}
	o.RunID++
	o.DryRun = true
	o.Out = filepath.Join(t.TempDir(), "unrelated")
	if _, err = r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Another document"}}, families()); err == nil || !strings.Contains(err.Error(), r.attemptPath(s, 0)) {
		t.Fatalf("verification accepted conflicting evidence for unsettled attempt: %v", err)
	}
}
