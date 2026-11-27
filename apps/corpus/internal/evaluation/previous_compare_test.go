package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func reportObject(t *testing.T, r Report) map[string]any {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func encodeObject(t *testing.T, object map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRun_PreviousWarningRecordsIgnored(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	baseline, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, developmentSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"wrong top-level shape", "not an array"},
		{"wrong record shape", []any{42}},
		{"removed overrides inside record", []any{map[string]any{"historical_policy": "agent", "merge_siblings": true}}},
		{"malformed nested match", []any{map[string]any{"search": "not an array"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := reportObject(t, baseline)
			object["warning_cases"] = tc.value
			data := encodeObject(t, object)
			path := filepath.Join(t.TempDir(), "previous.json")
			if writeErr := os.WriteFile(path, data, 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
			current, _, runErr := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, developmentSuite(), Options{EvaluationDate: date, PreviousReport: path})
			if runErr != nil {
				t.Fatal(runErr)
			}
			previous, compareErr := current.ComparedRelease()
			if compareErr != nil || previous == nil || previous.ReportSHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
				t.Fatalf("previous = %+v, error %v", previous, compareErr)
			}
			if len(current.NonRegression) == 0 || current.NonRegression[0].Status == "no_previous" {
				t.Fatal("comparison not run")
			}
		})
	}
	object := reportObject(t, baseline)
	object["warning_cases"] = json.RawMessage(`[1e10000]`)
	if _, err = decodePreviousReport(encodeObject(t, object)); err == nil || !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("non-finite warning record: error %v", err)
	}
	object = reportObject(t, baseline)
	object["outside"] = map[string]any{"historical_policy": "agent"}
	if _, err = decodePreviousReport(encodeObject(t, object)); err == nil {
		t.Fatal("outside removed override accepted")
	}
}

func TestRun_RefusesIncompatiblePreviousReport(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	baseline, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, developmentSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"compatible", func(map[string]any) {}},
		{"version zero", func(o map[string]any) { o["version"] = 0 }},
		{"version six", func(o map[string]any) { o["version"] = 6 }},
		{"unknown scorer", func(o map[string]any) { o["scorer"] = "agent-eval/7" }},
		{"legacy fixture differs", func(o map[string]any) { o["fixture_hashes"].(map[string]any)["Legacy"] = "other" }},
		{"old product fixture differs", func(o map[string]any) { o["fixture_hashes"].(map[string]any)["OldProduct"] = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := reportObject(t, baseline)
			tc.mutate(object)
			path := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(path, encodeObject(t, object), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, developmentSuite(), Options{EvaluationDate: date, PreviousReport: path})
			if tc.name == "compatible" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || (!strings.Contains(err.Error(), "incompatible previous scorer or fixtures") && !strings.Contains(err.Error(), "pairing")) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDecodePreviousReport_ValidatesRevision(t *testing.T) {
	previous := []revisionCase{
		{id: "a", r8: []int{1}, r20: []int{1}},
		{id: "x", r8: []int{1}, r20: []int{1}},
	}
	current := []revisionCase{
		{id: "a", r8: []int{1}, r20: []int{1}},
		{id: "x", r8: []int{1}, r20: []int{1}, question: "changed"},
	}
	p, r := revisionPair(t, previous, current)
	if err := finishQuality(&r, &p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"valid", func(map[string]any) {}, ""},
		{"null value", func(rv map[string]any) { rv["value"] = nil }, ""},
		{"cases missing", func(rv map[string]any) { delete(rv, "cases") }, "invalid previous revision cases"},
		{"value missing", func(rv map[string]any) { delete(rv, "value") }, "previous revision missing value"},
		{"revised missing", func(rv map[string]any) { delete(rv, "revised") }, "invalid previous revision revised"},
		{"empty case id", func(rv map[string]any) { rv["revised"] = []any{map[string]any{"case_id": "", "reason": "changed"}} }, "invalid previous revised case"},
		{"unknown reason", func(rv map[string]any) { rv["revised"] = []any{map[string]any{"case_id": "x", "reason": "renamed"}} }, "invalid previous revised case"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := reportObject(t, r)
			development := object["cohorts"].(map[string]any)["development"].(map[string]any)
			found := 0
			for _, entry := range development["non_regression"].([]any) {
				if rv, ok := entry.(map[string]any)["revision"].(map[string]any); ok {
					found++
					if found == 1 {
						tc.mutate(rv)
					}
				}
			}
			if found != 4 {
				t.Fatalf("revision records = %d", found)
			}
			_, err := decodePreviousReport(encodeObject(t, object))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}
