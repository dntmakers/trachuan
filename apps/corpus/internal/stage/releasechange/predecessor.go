package releasechange

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

type Predecessor struct {
	Release        string
	FindingsSHA256 string
	Findings       *FindingsFile
	IdentityPath   string
	IdentitySHA256 string
}

type predecessorQueries interface {
	GetRun(context.Context, store.GetRunParams) (store.Run, error)
	GetBuildInventory(context.Context, int64) (json.RawMessage, error)
}

func FrozenReportRelease(artifacts artifact.Store, decision stage.PreviousReport) (string, error) {
	if decision.None() {
		return noneValue, nil
	}
	if decision.Path == "" {
		return "", refuse("previous report path missing")
	}
	data, err := os.ReadFile(filepath.Join(artifacts.Abs(decision.Path), stage.PreviousReportFile))
	if err != nil {
		return "", refuse("read previous report: %v", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != decision.SHA256 {
		return "", refuse("previous report hash mismatch")
	}
	report, err := evaluation.ParseDecisionReport(data)
	if err != nil {
		return "", refuse("parse previous report: %v", err)
	}
	return report.Rel, nil
}

func ResolvePredecessor(ctx context.Context, q predecessorQueries, artifacts artifact.Store, run stage.Run, decision stage.PreviousReport, rel string) (Predecessor, error) {
	out := Predecessor{Release: noneValue, FindingsSHA256: noneValue, IdentitySHA256: noneValue}
	if decision.None() {
		if rel != "" && rel != noneValue {
			return Predecessor{}, refuse("previous report decision is none")
		}
		return out, nil
	}
	if rel == "" || rel == noneValue {
		return Predecessor{}, refuse("predecessor release missing")
	}
	row, err := q.GetRun(ctx, store.GetRunParams{Country: run.Country, Rel: rel})
	if errors.Is(err, pgx.ErrNoRows) {
		return Predecessor{}, refuse("predecessor %s/%s has no run", run.Country, rel)
	}
	if err != nil {
		return Predecessor{}, fmt.Errorf("read predecessor run: %w", err)
	}
	var snapshot stage.Snapshot
	if err = json.Unmarshal(row.Snapshot, &snapshot); err != nil {
		return Predecessor{}, refuse("decode predecessor snapshot: %v", err)
	}
	out.Release = rel
	if stage.PredatesDecision(run.Country, rel, snapshot) {
		return out, nil
	}
	if snapshot.PreviousReport == nil {
		return Predecessor{}, refuse("predecessor %s has no previous report decision", rel)
	}
	if row.State != "evaluated" && row.State != "released" {
		return Predecessor{}, refuse("predecessor %s is %s, not evaluated", rel, row.State)
	}
	raw, err := q.GetBuildInventory(ctx, row.ID)
	if err != nil {
		return Predecessor{}, fmt.Errorf("read predecessor inventory: %w", err)
	}
	var inv struct {
		Items []struct {
			Stage          string  `json:"stage"`
			ItemKey        string  `json:"item_key"`
			State          string  `json:"state"`
			Path           *string `json:"path"`
			ArtifactSHA256 *string `json:"artifact_sha256"`
		} `json:"items"`
	}
	if err = json.Unmarshal(raw, &inv); err != nil {
		return Predecessor{}, refuse("decode predecessor inventory: %v", err)
	}
	var path, expected string
	for _, item := range inv.Items {
		if item.Stage == stage.EvidenceStage && item.ItemKey == row.Country+"/"+row.Rel && item.State == "done" && item.Path != nil && item.ArtifactSHA256 != nil {
			path, expected = *item.Path, *item.ArtifactSHA256
			break
		}
	}
	if path == "" {
		return Predecessor{}, refuse("%s/%s has no done evidence item", row.Country, row.Rel)
	}
	dir := artifacts.Abs(path)
	verified, err := artifact.Verify(dir)
	if err != nil || verified != expected {
		return Predecessor{}, refuse("evidence artifact failed verification")
	}
	data, err := os.ReadFile(filepath.Join(dir, "findings.json"))
	if err != nil {
		return Predecessor{}, refuse("read findings: %v", err)
	}
	findings, err := DecodeFindings(data)
	if err != nil {
		return Predecessor{}, err
	}
	if findings.Bindings.Rel != rel || findings.Bindings.ReportSHA256 != hashPrefix+decision.SHA256 {
		return Predecessor{}, refuse("predecessor %s evidence does not bind the frozen previous report", rel)
	}
	sum := sha256.Sum256(data)
	out.FindingsSHA256 = hex.EncodeToString(sum[:])
	out.Findings = &findings
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.sha256"))
	if err != nil {
		return Predecessor{}, refuse("read evidence manifest: %v", err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if len(line) > 66 && line[64:66] == "  " && line[66:] == identityFile {
			out.IdentitySHA256 = line[:64]
			break
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(out.IdentitySHA256) {
		return Predecessor{}, refuse("predecessor %s evidence has no identity.jsonl", rel)
	}
	out.IdentityPath = filepath.Join(dir, identityFile)
	f, err := os.Open(out.IdentityPath)
	if err != nil {
		return Predecessor{}, refuse("open identity.jsonl: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<12), lineLimit+1)
	if !scanner.Scan() {
		return Predecessor{}, refuse("identity.jsonl has no header")
	}
	var header IdentityHeader
	if err = StrictDecode(scanner.Bytes(), &header); err != nil {
		return Predecessor{}, refuse("identity.jsonl header: %v", err)
	}
	if header.Schema != summarySchema {
		return Predecessor{}, refuse("predecessor %s identity schema %d is not %d", rel, header.Schema, summarySchema)
	}
	if header.Bindings.Rel != rel || header.Bindings.ReportSHA256 != hashPrefix+decision.SHA256 {
		return Predecessor{}, refuse("predecessor %s identity does not bind the frozen previous report", rel)
	}
	return out, nil
}
