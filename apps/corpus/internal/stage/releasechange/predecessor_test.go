package releasechange

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/internal/retrieval"
	"github.com/jackc/pgx/v5"
)

type predecessorFake struct {
	row       store.Run
	inventory json.RawMessage
	missing   bool
}

func (f predecessorFake) GetRun(context.Context, store.GetRunParams) (store.Run, error) {
	if f.missing {
		return store.Run{}, pgx.ErrNoRows
	}
	return f.row, nil
}
func (f predecessorFake) GetBuildInventory(context.Context, int64) (json.RawMessage, error) {
	return f.inventory, nil
}

func TestResolvePredecessor_NoneAndPredates(t *testing.T) {
	got, err := ResolvePredecessor(context.Background(), nil, artifact.Store{}, stage.Run{}, stage.PreviousReport{SHA256: stage.NoPreviousReport}, "")
	if err != nil || got.Release != "none" || got.IdentitySHA256 != "none" || got.FindingsSHA256 != "none" {
		t.Fatalf("got %+v, error %v", got, err)
	}
	fake := predecessorFake{row: store.Run{Country: "vn", Rel: "20261118_1", Snapshot: json.RawMessage(`{}`)}}
	got, err = ResolvePredecessor(context.Background(), fake, artifact.Store{}, stage.Run{Country: "vn"}, stage.PreviousReport{SHA256: strings.Repeat("a", 64)}, fake.row.Rel)
	if err != nil || got.Release != fake.row.Rel || got.IdentitySHA256 != "none" {
		t.Fatalf("got %+v, error %v", got, err)
	}
}

func TestResolvePredecessor_ArtifactBindings(t *testing.T) {
	const rel = "20990101_1"
	report := strings.Repeat("a", 64)
	cases := []struct {
		name                      string
		findingSchema             int
		findingRel, findingHash   string
		identitySchema            int
		identityRel, identityHash string
		omitIdentity, badArtifact bool
	}{
		{"valid", 2, rel, report, 1, rel, report, false, false},
		{"malformed findings", 2, rel, report, 1, rel, report, false, false},
		{"finding schema", 3, rel, report, 1, rel, report, false, false},
		{"finding release", 2, "other", report, 1, rel, report, false, false},
		{"finding hash", 2, rel, "bad", 1, rel, report, false, false},
		{"identity schema", 2, rel, report, 2, rel, report, false, false},
		{"identity release", 2, rel, report, 1, "other", report, false, false},
		{"identity hash", 2, rel, report, 1, rel, "bad", false, false},
		{"missing identity", 2, rel, report, 1, rel, report, true, false},
		{"artifact hash", 2, rel, report, 1, rel, report, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artifacts := artifact.Store{Root: t.TempDir()}
			findings := fmt.Sprintf(`{"schema":%d,"bindings":{"rel":%q,"report_sha256":"sha256:%s"}}`, tc.findingSchema, tc.findingRel, tc.findingHash)
			if tc.name == "malformed findings" {
				findings = strings.TrimSuffix(findings, "}") + `,"findings":"bad"}`
			}
			identity := fmt.Sprintf(`{"schema":%d,"bindings":{"rel":%q,"dump_sha256":"sha256:x","build_artifact_sha256":"sha256:y","report_sha256":"sha256:%s"}}`, tc.identitySchema, tc.identityRel, tc.identityHash)
			art, err := artifacts.Publish(context.Background(), "evidence", strings.Repeat("b", 64), strings.Repeat("c", 64), func(dir string) error {
				if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte(findings), 0600); err != nil {
					return err
				}
				if !tc.omitIdentity {
					return os.WriteFile(filepath.Join(dir, "identity.jsonl"), []byte(identity+"\n"), 0600)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			digest := art.SHA256
			if tc.badArtifact {
				digest = strings.Repeat("0", 64)
			}
			raw, err := json.Marshal(map[string]any{"items": []any{map[string]any{"stage": "evidence", "item_key": "vn/" + rel, "state": "done", "path": art.Path, "artifact_sha256": digest}}})
			if err != nil {
				t.Fatal(err)
			}
			fake := predecessorFake{row: store.Run{ID: 1, Country: "vn", Rel: rel, State: "evaluated", Snapshot: json.RawMessage(`{"PreviousReport":{"sha256":"none"}}`)}, inventory: raw}
			got, err := ResolvePredecessor(context.Background(), fake, artifacts, stage.Run{Country: "vn"}, stage.PreviousReport{SHA256: report}, rel)
			if tc.name == "valid" {
				sum := sha256.Sum256([]byte(identity + "\n"))
				if err != nil || got.IdentitySHA256 != hex.EncodeToString(sum[:]) || got.IdentityPath == "" || got.FindingsSHA256 != sha(findings) {
					t.Fatalf("got %+v, error %v", got, err)
				}
				return
			}
			var input *InputError
			if !errors.As(err, &input) {
				t.Fatalf("expected InputError, got %v", err)
			}
		})
	}
}

func TestResolvePredecessor_RejectsMissingRunAndEvidence(t *testing.T) {
	row := store.Run{ID: 1, Country: "vn", Rel: "20990101_1", State: "evaluated", Snapshot: json.RawMessage(`{"PreviousReport":{"sha256":"none"}}`)}
	for name, fake := range map[string]predecessorFake{
		"missing run":         {missing: true},
		"missing item":        {row: row, inventory: json.RawMessage(`{"items":[]}`)},
		"unreadable artifact": {row: row, inventory: json.RawMessage(`{"items":[{"stage":"evidence","item_key":"vn/20990101_1","state":"done","path":"missing","artifact_sha256":"bad"}]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolvePredecessor(context.Background(), fake, artifact.Store{Root: t.TempDir()}, stage.Run{Country: "vn"}, stage.PreviousReport{SHA256: strings.Repeat("a", 64)}, row.Rel)
			var input *InputError
			if !errors.As(err, &input) {
				t.Fatalf("expected InputError, got %v", err)
			}
		})
	}
}

func TestFrozenReportRelease_VerifiesBytes(t *testing.T) {
	artifacts := artifact.Store{Root: t.TempDir()}
	rel := "20990101_1"
	recipe := retrieval.Recipe3()
	raw, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprintf(`{"version":2,"scorer":"agent-eval/5","status":"complete","country":"vn","rel":%q,"retrieval_recipe":%s,"recipes":{"retrieval":%q}}`, rel, raw, recipe.Hash())
	dir := filepath.Join(artifacts.Root, "report")
	if mkdirErr := os.Mkdir(dir, 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, stage.PreviousReportFile), []byte(report), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	decision := stage.PreviousReport{SHA256: sha(report), Path: "report"}
	got, err := FrozenReportRelease(artifacts, decision)
	if err != nil || got != rel {
		t.Fatalf("got %q, error %v", got, err)
	}
	decision.SHA256 = strings.Repeat("0", 64)
	if _, mismatchErr := FrozenReportRelease(artifacts, decision); mismatchErr == nil {
		t.Fatal("accepted incorrect hash")
	}
	got, err = FrozenReportRelease(artifacts, stage.PreviousReport{SHA256: stage.NoPreviousReport})
	if err != nil || got != "none" {
		t.Fatalf("got %q, error %v", got, err)
	}
}
