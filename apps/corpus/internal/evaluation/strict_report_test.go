package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pythonDecodeRefuses(t *testing.T, inputs [][]byte) []bool {
	t.Helper()
	texts := make([][]byte, len(inputs))
	copy(texts, inputs)
	data, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	script := `import base64,json,sys
from corpus_manifest import decode
out = []
for text in json.load(sys.stdin):
    try:
        decode(base64.b64decode(text))
        out.append(False)
    except ValueError:
        out.append(True)
print(json.dumps(out))`
	command := exec.CommandContext(context.Background(), "python3", "-c", script)
	command.Dir = filepath.Join("..", "..", "..", "..", "deploy", "github")
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+command.Dir)
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python decode: %v: %s", err, output)
	}
	var results []bool
	if err = json.Unmarshal(output, &results); err != nil || len(results) != len(inputs) {
		t.Fatalf("Python decode output %q: %v", output, err)
	}
	return results
}

type duplicateKeyCase struct {
	name   string
	mutate func(string) string
}

func duplicateKeyCases() []duplicateKeyCase {
	inject := func(old, new string) func(string) string {
		return func(text string) string {
			if !strings.Contains(text, old) {
				panic("missing " + old)
			}
			return strings.Replace(text, old, new, 1)
		}
	}
	return []duplicateKeyCase{
		{"top level", inject(`"status":"complete"`, `"status":"incomplete","status":"complete"`)},
		{"retrieval recipe", inject(`"retrieval_recipe":{`, `"retrieval_recipe":{"version":99,`)},
		{"recipe hashes", inject(`"recipes":{"retrieval":`, `"recipes":{"retrieval":"x","retrieval":`)},
		{"cohort", inject(`"rejection_count":0`, `"rejection_count":9,"rejection_count":0`)},
		{"floor", inject(`"floor":0.99`, `"floor":0.5,"floor":0.99`)},
		{"case row in array", inject(`"case_id":"p1"`, `"case_id":"x","case_id":"p1"`)},
		{"empty key", inject(`"rel":"20990101_1"`, `"":1,"":2,"rel":"20990101_1"`)},
	}
}

func releaseReportDuplicateKeyCases() []duplicateKeyCase {
	return []duplicateKeyCase{
		{"top level", duplicateKeyCases()[0].mutate},
		{"retrieval recipe", duplicateKeyCases()[1].mutate},
		{"cohort", duplicateKeyCases()[3].mutate},
		{"floor", duplicateKeyCases()[4].mutate},
		{"case row", func(text string) string {
			return strings.Replace(text, `"cases":[{`, `"cases":[{"case_id":"x",`, 1)
		}},
	}
}

func decisionReportWithCohorts(t *testing.T) string {
	t.Helper()
	floor := map[string]any{"mode": "exact", "arms": "hybrid", "limit": 8, "name": "current_law_precision", "numerator": 1, "denominator": 1, "value": 1}
	metric := map[string]any{}
	floorRecord := map[string]any{"floor": 0.99, "status": "pass"}
	for key, value := range floor {
		metric[key], floorRecord[key] = value, value
	}
	return string(decisionReport(t, func(r map[string]any) {
		r["cohorts"] = map[string]any{"old_product": map[string]any{"case_count": 1, "rejection_count": 0, "floors": []any{floorRecord}, "metrics": []any{metric}, "non_regression": []any{}}}
		r["floors"] = []any{floorRecord}
		r["non_regression"] = []any{}
		r["rejections"] = []any{}
		r["cases"] = []any{map[string]any{"case_id": "p1", "cohort": "old_product", "mode": "exact", "arms": "hybrid", "limit": 8, "expected": 1, "ranks": []any{1}}}
	}))
}

func TestDecisionReport_RefusesDuplicateKeysLikePython(t *testing.T) {
	base := decisionReportWithCohorts(t)
	if _, err := ParseDecisionReport([]byte(base)); err != nil {
		t.Fatalf("control report refused: %v", err)
	}
	cases := duplicateKeyCases()
	inputs := make([][]byte, len(cases))
	for i, tc := range cases {
		inputs[i] = []byte(tc.mutate(base))
	}
	refused := pythonDecodeRefuses(t, inputs)
	for i, tc := range cases {
		if !refused[i] {
			t.Fatalf("Python accepted %s", tc.name)
		}
		_, err := ParseDecisionReport(inputs[i])
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("Go %s: error = %v, want duplicate key refusal", tc.name, err)
		}
		if _, err = DecodeReport(inputs[i]); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("DecodeReport %s: error = %v, want duplicate key refusal", tc.name, err)
		}
	}
}

func TestDecodeReport_AcceptsRepeatedKeysInDifferentObjects(t *testing.T) {
	data := `{"status":"complete","cases":[{"case_id":"a"},{"case_id":"b"}],"cohorts":{"x":{"status":"y"},"z":{"status":"y"}}}`
	report, err := DecodeReport([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || len(report.Cases) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, bad := range []string{`{"a":1} {"a":2}`, `{"a":1,}`, `[`, ``} {
		if _, err = DecodeReport([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDecodeReport_RefusesDeepNesting(t *testing.T) {
	deep := strings.Repeat(`{"a":`, 5000) + `1` + strings.Repeat(`}`, 5000)
	if _, err := DecodeReport([]byte(deep)); err == nil {
		t.Fatal("accepted deeply nested report")
	}
}

func TestReadPreviousRecipe_RefusesDuplicateKeys(t *testing.T) {
	previous, err := ParseDecisionReport(decisionReport(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	metadata := `{"country":"vn","rel":"20990101_1","retrieval_recipe":` + string(previous.RetrievalRecipe) + `}`
	for name, text := range map[string]string{
		"control":        metadata,
		"top level":      strings.Replace(metadata, `"country":"vn"`, `"country":"xx","country":"vn"`, 1),
		"nested recipe":  strings.Replace(metadata, `"retrieval_recipe":{`, `"retrieval_recipe":{"version":99,`, 1),
		"recipes object": `{"country":"vn","rel":"20990101_1","recipes":{"retrieval":` + string(previous.RetrievalRecipe) + `,"retrieval":{}}}`,
	} {
		path := filepath.Join(t.TempDir(), "release.json")
		if err = os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		candidate := previous
		err = readPreviousRecipe(&candidate, path)
		if name == "control" {
			if err != nil {
				t.Fatalf("control refused: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("%s: error = %v, want duplicate key refusal", name, err)
		}
	}
}

func TestDecodeReport_RefusesCaseVariantKeysAndInvalidUTF8(t *testing.T) {
	variant := `{"status":"failed","Status":"complete"}`
	var plain struct{ Status string }
	if err := json.Unmarshal([]byte(variant), &plain); err != nil || plain.Status != "complete" {
		t.Fatalf("encoding/json no longer folds field names: %q, %v", plain.Status, err)
	}
	for name, data := range map[string]string{
		"case variant":      variant,
		"upper case":        `{"cases":[{"case_id":"a","CASE_ID":"b"}]}`,
		"unicode fold":      "{\"k\":1,\"\u212a\":2}",
		"invalid UTF-8":     "{\"status\":\"\xff\"}",
		"invalid UTF-8 key": "{\"\xc3\":1}",
	} {
		if _, err := DecodeReport([]byte(data)); err == nil {
			t.Fatalf("%s accepted", name)
		}
		if _, err := decodePreviousReport([]byte(data)); err == nil {
			t.Fatalf("%s accepted as a previous report", name)
		}
	}
	refused := pythonDecodeRefuses(t, [][]byte{[]byte("{\"status\":\"\xff\"}"), []byte("{\"\xc3\":1}")})
	if !refused[0] || !refused[1] {
		t.Fatalf("Python accepted invalid UTF-8: %v", refused)
	}
	if python := pythonDecodeRefuses(t, [][]byte{[]byte(variant)}); python[0] {
		t.Fatal("Python now refuses case-variant keys; align the Go rule")
	}
}
