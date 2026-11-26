package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type previousTamper struct {
	name   string
	mutate func(*Report)
}

func previousRecipeForTamper(r *Report) retrieval.Recipe {
	var recipe retrieval.Recipe
	_ = json.Unmarshal(r.RetrievalRecipe, &recipe)
	return recipe
}

func previousTamperCases() []previousTamper {
	return []previousTamper{
		{"version4 recipe4", func(r *Report) {
			recipe := retrieval.Recipe4()
			recipe.Abstention = previousRecipeForTamper(r).Abstention
			encoded, _ := recipe.JSON()
			r.RetrievalRecipe, r.Recipes["retrieval"], r.RetrievalRecipeVersion = encoded, recipe.Hash(), 4
		}},
		{"version4 wrong query gate", func(r *Report) {
			recipe := previousRecipeForTamper(r)
			recipe.QueryGate = "vn-restore/1"
			encoded, _ := recipe.JSON()
			r.RetrievalRecipe, r.Recipes["retrieval"], r.RetrievalRecipeVersion = encoded, recipe.Hash(), 3
		}},
		{"version4 wrong document cap", func(r *Report) {
			recipe := previousRecipeForTamper(r)
			recipe.DocumentCap++
			encoded, _ := recipe.JSON()
			r.RetrievalRecipe, r.Recipes["retrieval"], r.RetrievalRecipeVersion = encoded, recipe.Hash(), 3
		}},
		{"version4 extra recipe field", func(r *Report) {
			recipe := previousRecipeForTamper(r)
			encoded, _ := recipe.JSON()
			var fields map[string]any
			_ = json.Unmarshal(encoded, &fields)
			fields["extra"] = true
			r.RetrievalRecipe, _ = json.Marshal(fields)
			r.Recipes["retrieval"], r.RetrievalRecipeVersion = recipe.Hash(), 3
		}},
		{"version4 scorer5", func(r *Report) {
			r.Scorer = "agent-eval/5"
		}},
		{"version4 recipe1", func(r *Report) {
			recipe := previousRecipeForTamper(r)
			recipe.Version = 1
			encoded, _ := recipe.JSON()
			r.RetrievalRecipe, r.Recipes["retrieval"], r.RetrievalRecipeVersion = encoded, recipe.Hash(), 1
		}},
		{"missing previous recall", func(r *Report) {
			c := r.Cohorts["old_product"]
			for i := range c.Metrics {
				if c.Metrics[i].Mode == "exact" && c.Metrics[i].Arms == "hybrid" && c.Metrics[i].Name == "recall" && c.Metrics[i].Limit == 20 {
					c.Metrics[i].Value = nil
				}
			}
			r.Cohorts["old_product"] = c
		}},
		{"wrong previous mrr", func(r *Report) {
			c := r.Cohorts["old_product"]
			for i := range c.Metrics {
				if c.Metrics[i].Mode == "exact" && c.Metrics[i].Arms == "hybrid" && c.Metrics[i].Name == "mrr" && c.Metrics[i].Limit == 8 {
					v := .25
					c.Metrics[i].Value = &v
				}
			}
			r.Cohorts["old_product"] = c
		}},
		{"duplicate precision", func(r *Report) {
			c := r.Cohorts["old_product"]
			for _, m := range c.Metrics {
				if m.Mode == "exact" && m.Arms == "hybrid" && m.Name == "current_law_precision" && m.Limit == 8 {
					c.Metrics = append(c.Metrics, m)
					break
				}
			}
			r.Cohorts["old_product"] = c
		}},
		{"missing precision", func(r *Report) {
			c := r.Cohorts["old_product"]
			metrics := []Metric{}
			for _, m := range c.Metrics {
				if m.Mode != "exact" || m.Arms != "hybrid" || m.Name != "current_law_precision" || m.Limit != 8 {
					metrics = append(metrics, m)
				}
			}
			c.Metrics = metrics
			for i := range c.Floors {
				if c.Floors[i].Mode == "exact" {
					c.Floors[i].Metric = findQualityMetric(r, "missing", "exact", "current_law_precision", 8)
					c.Floors[i].Status = "unavailable"
				}
			}
			r.Cohorts["old_product"] = c
			r.Floors = c.Floors
		}},
		{"duplicate development precision", func(r *Report) {
			c := r.Cohorts["development"]
			for _, m := range c.Metrics {
				if m.Mode == "exact" && m.Arms == "hybrid" && m.Name == "current_law_precision" && m.Limit == 8 {
					c.Metrics = append(c.Metrics, m)
					break
				}
			}
			r.Cohorts["development"] = c
		}},
		{"missing development precision", func(r *Report) {
			c := r.Cohorts["development"]
			metrics := []Metric{}
			for _, m := range c.Metrics {
				if m.Mode != "exact" || m.Arms != "hybrid" || m.Name != "current_law_precision" || m.Limit != 8 {
					metrics = append(metrics, m)
				}
			}
			c.Metrics = metrics
			for i := range c.Floors {
				if c.Floors[i].Mode == "exact" {
					c.Floors[i].Metric = findQualityMetric(r, "missing", "exact", "current_law_precision", 8)
					c.Floors[i].Status = "unavailable"
				}
			}
			r.Cohorts["development"] = c
		}},
	}
}

func TestReview_PreviousPairingAndMetrics(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	suite := developmentSuite()
	base, _, err := Run(context.Background(), &recordedEngine{fakeEngine: fakeEngine{date: date}}, suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range previousTamperCases() {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			var previous Report
			if err = json.Unmarshal(data, &previous); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&previous)
			data, err = json.Marshal(previous)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "previous.json")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			engine := &recipe4Engine{recordedEngine{fakeEngine: fakeEngine{date: date}}}
			if _, _, err = Run(context.Background(), engine, suite, Options{EvaluationDate: date, PreviousReport: path}); err == nil {
				t.Fatal("accepted invalid previous report")
			}
		})
	}
}
