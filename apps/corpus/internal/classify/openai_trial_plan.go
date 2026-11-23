package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
)

type openAITrialPlan struct {
	Recipe   Recipe   `json:"recipe"`
	Items    []Item   `json:"items"`
	Families []Family `json:"families"`
	Nodes    []Node   `json:"nodes"`
}

func ResumeOpenAITrial(ctx context.Context, stage string, o OpenAITrialOptions, client *OpenAITrialClient) (OpenAITrialSummary, bool, error) {
	var summary OpenAITrialSummary
	if stage != "documents" && stage != "articles" {
		return summary, false, fmt.Errorf("invalid OpenAI trial stage")
	}
	dir, err := filepath.Abs(o.Out)
	if err != nil || o.Out == "" || client == nil || client.Client == nil || client.guard([]byte(dir)) != nil || client.guard([]byte(client.ProjectID())) != nil {
		return summary, false, fmt.Errorf("invalid trial output directory")
	}
	t := openAITrial{o: o, client: client}
	t.o.Out = dir
	statePath := t.path("state.json")
	_, found, err := readTrialFile(statePath)
	if err != nil || !found {
		return summary, false, err
	}
	unlock, err := trialLock(dir)
	if err != nil {
		return summary, true, err
	}
	defer unlock()
	found, err = readTrialState(statePath, &t.state)
	if err != nil || !found {
		return summary, true, fmt.Errorf("cannot read retained trial state")
	}
	if !t.state.matchesSelection(o) {
		return summary, true, fmt.Errorf("trial selection does not match retained state")
	}
	if o.Unseeded && o.LabelsHash == "" {
		switch t.state.Phase {
		case trialDispatched, trialSubmitted, trialTerminal, trialSettled:
			o.LabelsHash = t.state.LabelsHash
			t.o.LabelsHash = o.LabelsHash
		default:
			return summary, false, nil
		}
	}
	if t.state.Phase == trialPreparing {
		return summary, false, nil
	}
	expectedStage := "document"
	if stage == "articles" {
		expectedStage = "article"
	}
	if t.state.Model != o.Recipe.Model || t.state.Effort != o.Recipe.Effort || t.state.Stage != expectedStage || t.state.Country != o.Country || t.state.Release != o.Release || t.state.LabelsHash != o.LabelsHash || t.state.Project != client.ProjectID() || t.state.Fingerprint != client.AccountFingerprint() {
		return summary, true, fmt.Errorf("trial state does not match stage, inputs, recipe or credentials")
	}
	summary, err = t.resume(ctx)
	return summary, true, err
}

func (t *openAITrial) resume(ctx context.Context) (OpenAITrialSummary, error) {
	var summary OpenAITrialSummary
	if err := syncDirectory(t.o.Out); err != nil {
		return summary, err
	}
	if err := t.restorePlan(); err != nil {
		return summary, err
	}
	summary.Requests, summary.ReserveMicroUSD, summary.SiblingMicroUSD = len(t.parts), t.state.ReserveMicroUSD, t.state.SiblingMicroUSD
	if t.o.DryRun && t.o.Unseeded {
		summary.SelectedDocuments, summary.SelectedArticles = t.state.SelectedDocuments, t.state.SelectedArticles
	}
	for _, part := range t.parts {
		summary.CountedInputTokens += part.Count
	}
	switch t.state.Phase {
	case trialReserved, trialUploading, trialUploaded:
		if err := t.withTrialBudget(&summary, nil); err != nil {
			return summary, err
		}
	}
	if t.o.DryRun {
		return summary, nil
	}
	return summary, t.execute(ctx)
}

func (t *openAITrial) restorePlan() error {
	var plan openAITrialPlan
	if err := t.readBound("plan.json", t.state.PlanHash, &plan); err != nil {
		return err
	}
	input, err := t.boundBytes("input.jsonl", t.state.InputHash)
	if err != nil {
		return err
	}
	return t.restoreBoundPlan(plan, input, false)
}

func (t *openAITrial) restoreBoundPlan(plan openAITrialPlan, input []byte, strict bool) error {
	if plan.Recipe.Hash() != t.state.RecipeHash || plan.Recipe.Model != t.state.Model || plan.Recipe.Effort != t.state.Effort || (plan.Recipe.Article != nil) != (t.state.Stage == "article") || len(plan.Items) == 0 {
		return fmt.Errorf("retained trial plan recipe mismatch")
	}
	if t.o.ExpectedFamiliesHash != "" && (plan.Recipe.Article == nil || plan.Recipe.Article.FamiliesHash != t.o.ExpectedFamiliesHash) {
		return fmt.Errorf("families artifact differs from the retained trial recipe")
	}
	if _, err := openAIRecipe(plan.Recipe); err != nil {
		return err
	}
	t.o.Recipe, t.nodes = plan.Recipe, plan.Nodes
	t.result = Submission{Country: t.state.Country, Release: t.state.Release, Recipe: plan.Recipe, Items: plan.Items, Families: plan.Families, Results: map[string]Outcome{}}
	t.input = input
	var reserve int64
	rest := t.input
	for i, item := range plan.Items {
		if t.state.Stage == "document" {
			t.result.Results[item.Key] = Validate("", "", plan.Recipe.F, plan.Families)
		}
		if item.Local {
			continue
		}
		line, tail, found := bytes.Cut(rest, []byte{'\n'})
		rest = tail
		var req struct {
			ID     string          `json:"custom_id"`
			Method string          `json:"method"`
			URL    string          `json:"url"`
			Body   json.RawMessage `json:"body"`
		}
		if strict {
			if _, err := closedObject(line, []string{"custom_id", "method", "url", "body"}); err != nil || packCheckJSON(line) != nil {
				return fmt.Errorf("invalid retained trial JSONL envelope")
			}
		}
		if !found || json.Unmarshal(line, &req) != nil || req.ID != strconv.Itoa(i) || req.Method != "POST" || req.URL != "/v1/responses" || openAICountKey(req.Body) != item.RequestHash {
			return fmt.Errorf("retained trial JSONL differs from plan")
		}
		partReserve, err := plan.Recipe.reserve(t.state.PriceVersion, item.InputTokens)
		if err != nil {
			return err
		}
		reserve += partReserve
		t.parts = append(t.parts, openAITrialItem{openaibatch.Item{ID: req.ID, Body: req.Body}, i, item.InputTokens})
	}
	if len(rest) != 0 || reserve != t.state.ReserveMicroUSD {
		return fmt.Errorf("retained trial input or reserve differs from plan")
	}
	return nil
}

func (t *openAITrial) boundBytes(name, wantHash string) ([]byte, error) {
	raw, found, err := readTrialFile(t.path(name))
	if err != nil || !found || digest(raw) != wantHash {
		return nil, fmt.Errorf("retained trial %s differs from state", name)
	}
	return raw, nil
}

func (t *openAITrial) readBound(name, wantHash string, dst any) error {
	raw, err := t.boundBytes(name, wantHash)
	if err != nil {
		return err
	}
	if validateRetainedJSON(raw, dst) != nil || json.Unmarshal(raw, dst) != nil {
		return fmt.Errorf("invalid retained trial %s", name)
	}
	return nil
}
