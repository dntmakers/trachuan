package run

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestFreezeStartExtractContract(t *testing.T) {
	all := []string{"discover", "fetch", "extract"}
	for _, tc := range []struct {
		name                     string
		snapshot                 stage.Snapshot
		stages                   []string
		existing                 bool
		wantContract, wantRecipe string
	}{
		{"new includes extract", stage.Snapshot{Stages: all}, all, false, stage.ExtractRecordV1, "extract/" + stage.ExtractRecordV1},
		{"new fetch only", stage.Snapshot{Stages: all[:2]}, all[:2], false, "", ""},
		{"deferred extension", stage.Snapshot{Stages: all[:2], Recipes: map[string]string{"discover": "d", "fetch": "f"}}, all, true, stage.ExtractRecordV1, "extract/" + stage.ExtractRecordV1},
		{"legacy preserved", stage.Snapshot{Stages: all, Recipes: map[string]string{"discover": "d", "fetch": "f", "extract": "extract/legacy"}, ExtractRecordContract: stage.ExtractRecordLegacy}, all, true, stage.ExtractRecordLegacy, "extract/legacy"},
		{"v1 repeat", stage.Snapshot{Stages: all, Recipes: map[string]string{"discover": "d", "fetch": "f", "extract": "extract/" + stage.ExtractRecordV1}, ExtractRecordContract: stage.ExtractRecordV1}, all, true, stage.ExtractRecordV1, "extract/" + stage.ExtractRecordV1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := freezeStartSnapshot(tc.snapshot, tc.stages, tc.existing, func(s stage.Snapshot) map[string]string {
				calls++
				if s.ExtractRecordContract != tc.wantContract {
					t.Errorf("RecipesFor saw contract %q want %q", s.ExtractRecordContract, tc.wantContract)
				}
				return map[string]string{"discover": "d", "fetch": "f", "extract": "extract/" + s.ExtractRecordContract}
			})
			if err != nil || calls != 1 || got.ExtractRecordContract != tc.wantContract || got.Recipes["extract"] != tc.wantRecipe || !reflect.DeepEqual(got.Stages, tc.stages) {
				t.Fatalf("snapshot=%+v calls=%d err=%v", got, calls, err)
			}
			if tc.wantRecipe == "" {
				if _, ok := got.Recipes["extract"]; ok {
					t.Fatal("froze extract recipe before extract")
				}
			}
		})
	}
}

func TestFreezeStartRejectsBrokenExtractAuthority(t *testing.T) {
	for _, contract := range []string{"", "unknown"} {
		snapshot := stage.Snapshot{Stages: []string{"fetch", "extract"}, Recipes: map[string]string{"fetch": "f", "extract": "frozen"}, ExtractRecordContract: contract}
		calls := 0
		_, err := freezeStartSnapshot(snapshot, snapshot.Stages, true, func(stage.Snapshot) map[string]string {
			calls++
			return map[string]string{"fetch": "f", "extract": "frozen"}
		})
		if err == nil || calls != 0 {
			t.Errorf("contract=%q calls=%d err=%v", contract, calls, err)
		}
	}
}

func TestFreezeStartRejectsRecipeMismatch(t *testing.T) {
	snapshot := stage.Snapshot{Stages: []string{"fetch", "extract"}, Recipes: map[string]string{"fetch": "f", "extract": "frozen"}, ExtractRecordContract: stage.ExtractRecordLegacy}
	_, err := freezeStartSnapshot(snapshot, snapshot.Stages, true, func(stage.Snapshot) map[string]string { return map[string]string{"fetch": "f", "extract": "changed"} })
	if err == nil {
		t.Fatal("accepted changed frozen recipe")
	}
}

func TestFreezeStartRejectsStrayExtractContract(t *testing.T) {
	snapshot := stage.Snapshot{Stages: []string{"fetch"}, Recipes: map[string]string{"fetch": "f"}, ExtractRecordContract: stage.ExtractRecordV1}
	calls := 0
	_, err := freezeStartSnapshot(snapshot, snapshot.Stages, true, func(stage.Snapshot) map[string]string { calls++; return map[string]string{"fetch": "f"} })
	if err == nil || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestFreezeStartRejectsUnpairedContract(t *testing.T) {
	snapshot := stage.Snapshot{Stages: []string{"obsolete"}, Recipes: map[string]string{"obsolete": "old"}}
	stages := []string{"fetch", "extract"}
	_, err := freezeStartSnapshot(snapshot, stages, true, func(stage.Snapshot) map[string]string {
		return map[string]string{"obsolete": "old", "fetch": "fetch", "extract": "extract"}
	})
	if err == nil {
		t.Fatal("accepted extract contract without a frozen extract recipe")
	}
}

func TestFreezeStartJSONContract(t *testing.T) {
	stages := []string{"fetch", "extract"}
	got, err := freezeStartSnapshot(stage.Snapshot{Stages: stages}, stages, false, func(snapshot stage.Snapshot) map[string]string {
		return map[string]string{"fetch": "fetch", "extract": "recipe/" + snapshot.ExtractRecordContract}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipes["extract"] != "recipe/"+stage.ExtractRecordV1 {
		t.Fatalf("extract recipe=%s", got.Recipes["extract"])
	}
	data, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(data), `"extract_record_contract":"detector-inputs/1"`) {
		t.Fatalf("snapshot=%s err=%v", data, err)
	}
	fetchOnly, err := freezeStartSnapshot(stage.Snapshot{Stages: stages[:1]}, stages[:1], false, func(stage.Snapshot) map[string]string { return map[string]string{"fetch": "fetch"} })
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(fetchOnly)
	if err != nil || strings.Contains(string(data), `"extract_record_contract"`) {
		t.Fatalf("snapshot=%s err=%v", data, err)
	}
}
