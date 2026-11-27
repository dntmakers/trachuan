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

	"danny.vn/trachuan/internal/retrieval"
)

type floorMutation struct {
	name   string
	want   string
	mutate func(root map[string]any)
}

func floorLists(root map[string]any, cohorts ...string) [][]any {
	lists := [][]any{}
	for _, name := range cohorts {
		lists = append(lists, root["cohorts"].(map[string]any)[name].(map[string]any)["floors"].([]any))
	}
	return lists
}

func floorRecords(root map[string]any) []map[string]any {
	records := []map[string]any{}
	for _, list := range append(floorLists(root, "old_product", "development"), root["floors"].([]any)) {
		for _, item := range list {
			records = append(records, item.(map[string]any))
		}
	}
	return records
}

func precisionMetrics(root map[string]any, cohort string) []map[string]any {
	found := []map[string]any{}
	metrics, _ := root["cohorts"].(map[string]any)[cohort].(map[string]any)["metrics"].([]any)
	for _, item := range metrics {
		metric := item.(map[string]any)
		if metric["name"] == "current_law_precision" && metric["arms"] == "hybrid" {
			found = append(found, metric)
		}
	}
	return found
}

func setEverywhere(root map[string]any, key string, value any) {
	for _, record := range floorRecords(root) {
		record[key] = value
	}
	for _, cohort := range []string{"old_product", "development"} {
		for _, metric := range precisionMetrics(root, cohort) {
			metric[key] = value
		}
	}
}

func exactMetric(root map[string]any) map[string]any {
	for _, metric := range precisionMetrics(root, "old_product") {
		if metric["mode"] == "exact" {
			return metric
		}
	}
	panic("no exact precision metric")
}

func floorCopies(root map[string]any) []map[string]any {
	return []map[string]any{floorLists(root, "old_product")[0][0].(map[string]any), root["floors"].([]any)[0].(map[string]any)}
}

func onFloorAndMetric(change func(record map[string]any)) func(map[string]any) {
	return func(root map[string]any) {
		for _, record := range floorCopies(root) {
			change(record)
		}
		change(exactMetric(root))
	}
}

func onFloorOnly(change func(record map[string]any)) func(map[string]any) {
	return func(root map[string]any) {
		for _, record := range floorCopies(root) {
			change(record)
		}
	}
}

func populatedFloorMutations() []floorMutation {
	swap := func(cohort string) func(map[string]any) {
		return func(root map[string]any) {
			floors := floorLists(root, cohort)[0]
			floors[0], floors[1] = floors[1], floors[0]
			if cohort == "old_product" {
				root["floors"] = root["cohorts"].(map[string]any)[cohort].(map[string]any)["floors"]
			}
		}
	}
	set := func(key string, value any) func(map[string]any) {
		return onFloorAndMetric(func(record map[string]any) { record[key] = value })
	}
	drop := func(key string) func(map[string]any) {
		return onFloorAndMetric(func(record map[string]any) { delete(record, key) })
	}
	return []floorMutation{
		{"swapped old product floors", "invalid exact precision floor", swap("old_product")},
		{"swapped development floors", "invalid exact precision floor", swap("development")},
		{"floor field reason", "unexpected field reason", set("reason", "x")},
		{"floor unknown field", "unexpected field unknown", set("unknown", true)},
		{"floor confusion", "unexpected field confusion", set("confusion", map[string]any{})},
		{"floor without value", "lacks value", drop("value")},
		{"floor without numerator", "lacks numerator", drop("numerator")},
		{"floor limit float", "cannot unmarshal", set("limit", json.Number("8.0"))},
		{"floor denominator float", "cannot unmarshal", set("denominator", json.Number("10.0"))},
		{"floor null name", "invalid name", set("name", nil)},
		{"floor not_yet_eligible null", "invalid not_yet_eligible", set("not_yet_eligible", nil)},
		{"floor mapping count float", "cannot unmarshal", set("mapping_incomplete_count", json.Number("1.5"))},
		{"floor mapping count string", "cannot unmarshal", set("mapping_incomplete_count", "1")},
		{"floor mapping count null", "invalid mapping_incomplete_count", set("mapping_incomplete_count", nil)},
		{"cap override everywhere", "research override", func(root map[string]any) { setEverywhere(root, "cap_override", 4) }},
		{"article cap override everywhere", "research override", func(root map[string]any) { setEverywhere(root, "article_cap_override", 4) }},
		{"non current tail everywhere", "research override", func(root map[string]any) { setEverywhere(root, "non_current_tail", 2) }},
		{"not yet eligible everywhere", "research override", func(root map[string]any) { setEverywhere(root, "not_yet_eligible", true) }},
		{"floor differs from metric: unknown field", "differs from its cohort metric", onFloorOnly(func(record map[string]any) { record["unknown"] = true })},
		{"floor differs from metric: value", "differs from its cohort metric", onFloorOnly(func(record map[string]any) { record["numerator"] = json.Number("7") })},
		{"metric differs from floor: reason", "differs from its cohort metric", func(root map[string]any) { exactMetric(root)["reason"] = "x" }},
		{"metric differs from floor: unknown", "differs from its cohort metric", func(root map[string]any) { exactMetric(root)["unknown"] = true }},
	}
}

func emptyDevelopmentFloorMutations() []floorMutation {
	record := func(root map[string]any) map[string]any {
		return floorLists(root, "development")[0][0].(map[string]any)
	}
	return []floorMutation{
		{"empty development reason", "unexpected field reason", func(root map[string]any) { record(root)["reason"] = "no cases" }},
		{"empty development unknown field", "unexpected field unknown", func(root map[string]any) { record(root)["unknown"] = 1 }},
		{"empty development swapped floors", "invalid empty development precision floor", func(root map[string]any) {
			floors := floorLists(root, "development")[0]
			floors[0], floors[1] = floors[1], floors[0]
		}},
		{"empty development without value", "lacks value", func(root map[string]any) { delete(record(root), "value") }},
		{"empty development null mode", "invalid mode", func(root map[string]any) { record(root)["mode"] = nil }},
	}
}

func floorBaselines(t *testing.T) map[string]Report {
	t.Helper()
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	populated, _, err := Run(context.Background(), &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}, developmentSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	suite := recordedSuite()
	addSecondLegacyPositive(t, &suite)
	empty, _, err := Run(context.Background(), &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if populated.Cohorts["development"].CaseCount == 0 || empty.Cohorts["development"].CaseCount != 0 {
		t.Fatalf("development case counts %d and %d", populated.Cohorts["development"].CaseCount, empty.Cohorts["development"].CaseCount)
	}
	return map[string]Report{"populated": populated, "empty": empty}
}

func goFloorError(data []byte) error {
	report, err := decodePreviousReport(data)
	if err != nil {
		return err
	}
	return validatePreviousPrecisions(&report)
}

func pythonRefusesFloors(t *testing.T, inputs [][]byte) []bool {
	t.Helper()
	texts := make([]string, len(inputs))
	for i, input := range inputs {
		texts[i] = string(input)
	}
	data, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	script := `import json,sys
from corpus_floors import check_floors
from corpus_manifest import decode
out = []
for text in json.load(sys.stdin):
    try:
        check_floors(decode(text.encode()))
        out.append(False)
    except (ValueError, KeyError, TypeError):
        out.append(True)
print(json.dumps(out))`
	command := exec.CommandContext(context.Background(), "python3", "-c", script)
	command.Dir = filepath.Join("..", "..", "..", "..", "deploy", "github")
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python floors: %v: %s", err, output)
	}
	var results []bool
	if err = json.Unmarshal(output, &results); err != nil || len(results) != len(inputs) {
		t.Fatalf("Python floors output %q: %v", output, err)
	}
	return results
}

func TestPreviousFloors_GoRefusesWhatPythonRefuses(t *testing.T) {
	type attempt struct {
		name, want string
		data       []byte
	}
	var attempts []attempt
	for kind, report := range floorBaselines(t) {
		mutations := populatedFloorMutations()
		if kind == "empty" {
			mutations = append(mutations, emptyDevelopmentFloorMutations()...)
		}
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		attempts = append(attempts, attempt{kind + " control", "", encoded})
		for _, mutation := range mutations {
			if kind == "empty" && mutation.name == "swapped development floors" {
				continue
			}
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			var root map[string]any
			if err = decoder.Decode(&root); err != nil {
				t.Fatal(err)
			}
			mutation.mutate(root)
			data, marshalErr := json.Marshal(root)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			attempts = append(attempts, attempt{kind + " " + mutation.name, mutation.want, data})
		}
	}
	inputs := make([][]byte, len(attempts))
	for i, a := range attempts {
		inputs[i] = a.data
	}
	python := pythonRefusesFloors(t, inputs)
	for i, a := range attempts {
		control := len(a.name) > 7 && a.name[len(a.name)-7:] == "control"
		if python[i] == control {
			t.Fatalf("Python on %s: refused %v", a.name, python[i])
		}
		err := goFloorError(a.data)
		if control {
			if err != nil {
				t.Fatalf("Go refused %s: %v", a.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), a.want) {
			t.Fatalf("Go on %s: error = %v, want %q", a.name, err, a.want)
		}
	}
}
