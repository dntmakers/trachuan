package run

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestFreezeStartLegacyEvidenceAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, contract                 string
		hasEvidence, decision, wantErr bool
	}{
		{"legacy add", stage.ExtractRecordLegacy, false, true, true},
		{"legacy historical", stage.ExtractRecordLegacy, true, true, false},
		{"legacy no decision", stage.ExtractRecordLegacy, false, false, false},
		{"v1 add", stage.ExtractRecordV1, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stages := []string{"fetch", "extract"}
			snapshot := stage.Snapshot{Stages: append([]string{}, stages...), ExtractRecordContract: tc.contract, Recipes: map[string]string{"fetch": "f", "extract": "e"}}
			if tc.decision {
				snapshot.PreviousReport = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
			}
			if tc.hasEvidence {
				snapshot.Stages = append(snapshot.Stages, stage.EvidenceStage)
				snapshot.Recipes[stage.EvidenceStage] = "v"
			}
			target := append(append([]string{}, stages...), stage.EvidenceStage)
			calls := 0
			got, err := freezeStartSnapshot(snapshot, target, true, func(stage.Snapshot) map[string]string {
				calls++
				return map[string]string{"fetch": "f", "extract": "e", stage.EvidenceStage: "v"}
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("snapshot=%+v err=%v", got, err)
			}
			if tc.wantErr && (calls != 0 || !strings.Contains(err.Error(), "legacy")) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if !tc.wantErr && (calls != 1 || got.ExtractRecordContract != tc.contract || containsStage(got.Stages, stage.EvidenceStage) != (tc.hasEvidence || (tc.decision && tc.contract == stage.ExtractRecordV1))) {
				t.Fatalf("snapshot=%+v calls=%d", got, calls)
			}
		})
	}
}

func TestFreezeStartLegacyNonEvidenceOperation(t *testing.T) {
	stages := []string{"fetch", "extract"}
	snapshot := stage.Snapshot{Stages: stages, ExtractRecordContract: stage.ExtractRecordLegacy, Recipes: map[string]string{"fetch": "f", "extract": "e"}, PreviousReport: &stage.PreviousReport{SHA256: stage.NoPreviousReport}}
	got, err := freezeStartSnapshot(snapshot, stages, true, func(stage.Snapshot) map[string]string { return map[string]string{"fetch": "f", "extract": "e"} })
	if err != nil || got.ExtractRecordContract != stage.ExtractRecordLegacy || containsStage(got.Stages, stage.EvidenceStage) {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
}
