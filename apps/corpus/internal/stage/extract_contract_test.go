package stage

import (
	"strings"
	"testing"
)

func TestExtractContractRequiresTrustedDeclaration(t *testing.T) {
	recipe := strings.Repeat("a", 64)
	for _, tc := range []struct {
		snapshot Snapshot
		verified string
		want     string
		ok       bool
	}{
		{Snapshot{ExtractRecordContract: ExtractRecordLegacy, Recipes: map[string]string{"extract": recipe}}, recipe, ExtractRecordLegacy, true},
		{Snapshot{ExtractRecordContract: ExtractRecordV1, Recipes: map[string]string{"extract": recipe}}, recipe, ExtractRecordV1, true},
		{Snapshot{Recipes: map[string]string{"extract": recipe}}, recipe, "", false},
		{Snapshot{ExtractRecordContract: "unknown", Recipes: map[string]string{"extract": recipe}}, recipe, "", false},
		{Snapshot{ExtractRecordContract: ExtractRecordLegacy}, recipe, "", false},
		{Snapshot{ExtractRecordContract: ExtractRecordLegacy, Recipes: map[string]string{"extract": recipe}}, strings.Repeat("b", 64), "", false},
		{Snapshot{ExtractRecordContract: ExtractRecordLegacy, Recipes: map[string]string{"extract": ""}}, "", "", false},
	} {
		got, err := ExtractContract(tc.snapshot, tc.verified)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("snapshot=%+v got=%q err=%v", tc.snapshot, got, err)
		}
	}
}
