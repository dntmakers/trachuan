package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestScreenOptions_ExplicitBudgets(t *testing.T) {
	base := []string{"--documents", "20", "--seed", "42", "--out", "synthetic-output", "--dry-run"}
	for _, tc := range []struct {
		budget, dollars, model string
		microUSD               int64
	}{
		{"pilot", "15", "claude-haiku-5-5", 15000000},
		{"trial", "200", "claude-fable-5-2", 200000000},
	} {
		t.Run(tc.budget, func(t *testing.T) {
			o, err := parseScreenOptions("vn", "20990101", append(append([]string{}, base...), "--recipe", tc.model, "--budget", tc.budget, "--budget-usd", tc.dollars))
			if err != nil || o.Documents != 20 || o.Seed != 42 || o.Budget != tc.budget || o.BudgetMicroUSD != tc.microUSD || o.Recipe.Model != tc.model || !o.DryRun {
				t.Fatalf("options %+v error %v", o, err)
			}
		})
	}
	valid := []string{"--documents", "20", "--seed", "42", "--recipe", "claude-haiku-5-5", "--out", "synthetic-output", "--budget", "pilot", "--budget-usd", "15", "--dry-run"}
	for _, args := range [][]string{nil, valid[:10], append(append([]string(nil), valid...), "--budget-usd", "15"), {"--documents", "401", "--seed", "1", "--recipe", "claude-haiku-5-5", "--out", "synthetic-output", "--budget", "pilot", "--budget-usd", "15"}} {
		if _, err := parseScreenOptions("vn", "20990101", args); err == nil {
			t.Fatalf("accepted invalid options %v", args)
		}
	}
	for _, pair := range [][2]string{{"pilot", "200"}, {"trial", "15"}, {"trial", "201"}, {"full", "100"}, {"cap", "150"}} {
		if _, err := parseScreenOptions("vn", "20990101", append(append([]string{}, base...), "--recipe", "claude-haiku-5-5", "--budget", pair[0], "--budget-usd", pair[1])); err == nil {
			t.Fatalf("accepted budget pair %v", pair)
		}
	}
	var out, errout bytes.Buffer
	if code := Run(context.Background(), []string{"classify", "screen"}, &out, &errout); code != 2 || !strings.Contains(errout.String(), "--budget pilot --budget-usd 15") || !strings.Contains(errout.String(), "--budget trial --budget-usd 200") {
		t.Fatalf("code %d error %s", code, &errout)
	}
}

func TestScreenOptions_ExclusiveSelectionOrDraw(t *testing.T) {
	base := []string{"--recipe", "claude-haiku-5-5", "--out", "output", "--budget", "pilot", "--budget-usd", "15", "--dry-run"}
	for _, tc := range []struct {
		name      string
		flags     []string
		wantError bool
	}{
		{"labels", []string{"--select", "labels:labels.json"}, false},
		{"draw", []string{"--documents", "2", "--seed", "0"}, false},
		{"neither", nil, true},
		{"selection and documents", []string{"--select", "labels:l", "--documents", "2"}, true},
		{"selection and zero seed", []string{"--select", "labels:l", "--seed", "0"}, true},
		{"selection and draw", []string{"--select", "labels:l", "--documents", "2", "--seed", "1"}, true},
		{"empty selection with draw", []string{"--select", "", "--documents", "2", "--seed", "1"}, true},
		{"empty labels", []string{"--select", "labels:"}, true},
		{"other selection", []string{"--select", "all"}, true},
		{"documents only", []string{"--documents", "2"}, true},
		{"seed only", []string{"--seed", "1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseScreenOptions("vn", "20990101", append(append([]string{}, base...), tc.flags...))
			if (err != nil) != tc.wantError || (tc.wantError && strings.Contains(err.Error(), "unknown option")) {
				t.Fatalf("error %v, want error %v", err, tc.wantError)
			}
		})
	}
}
