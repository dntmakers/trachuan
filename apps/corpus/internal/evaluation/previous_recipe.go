package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"danny.vn/trachuan/internal/retrieval"
)

func readPreviousReport(path, metadata string) (Report, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, "", fmt.Errorf("read previous report: %w", err)
	}
	previous, err := parsePreviousReport(data)
	if err != nil {
		return Report{}, "", err
	}
	if metadata != "" {
		if err = readPreviousRecipe(&previous, metadata); err != nil {
			return Report{}, "", err
		}
	}
	sum := sha256.Sum256(data)
	return previous, hex.EncodeToString(sum[:]), nil
}

func parsePreviousReport(data []byte) (Report, error) {
	previous, err := decodePreviousReport(data)
	if err != nil {
		return Report{}, err
	}
	if previous.Version >= 2 && len(previous.RetrievalRecipe) == 0 {
		return Report{}, fmt.Errorf("previous version %d retrieval recipe missing", previous.Version)
	}
	var recipe retrieval.Recipe
	if len(previous.RetrievalRecipe) > 0 {
		if err = json.Unmarshal(previous.RetrievalRecipe, &recipe); err != nil {
			return Report{}, fmt.Errorf("decode previous retrieval recipe: %w", err)
		}
		if !recipe.Supported() {
			return Report{}, fmt.Errorf("unsupported previous retrieval recipe")
		}
		if err = validatePreviousRecipeFields(previous.RetrievalRecipe, recipe); err != nil {
			return Report{}, err
		}
		if recipe.Hash() != previous.Recipes["retrieval"] {
			return Report{}, fmt.Errorf("previous retrieval recipe hash differs from report")
		}
	}
	if err = validatePreviousPairing(previous.Version, previous.Scorer, recipe.Version); err != nil {
		return Report{}, err
	}
	return previous, nil
}

func validatePreviousRecipeFields(raw json.RawMessage, recipe retrieval.Recipe) error {
	encoded, err := recipe.JSON()
	if err != nil {
		return fmt.Errorf("encode previous retrieval recipe: %w", err)
	}
	supplied, err := decodeNumbers(raw)
	if err != nil {
		return fmt.Errorf("decode supplied retrieval recipe: %w", err)
	}
	known, err := decodeNumbers(encoded)
	if err != nil {
		return fmt.Errorf("decode known retrieval recipe: %w", err)
	}
	if !jsonEqual(supplied, known, true) {
		return fmt.Errorf("previous retrieval recipe has unknown or missing fields")
	}
	return nil
}

func ParseDecisionReport(data []byte) (Report, error) {
	report, err := parsePreviousReport(data)
	if err != nil {
		return Report{}, err
	}
	if report.Version != 2 && report.Version != 3 && report.Version != 4 && report.Version != 5 {
		return Report{}, fmt.Errorf("previous report is version %d, want version 2, 3, 4 or 5", report.Version)
	}
	if report.Status != "complete" {
		return Report{}, fmt.Errorf("previous report status %q is not complete", report.Status)
	}
	return report, nil
}

type ComparedRelease struct {
	Country      string `json:"country"`
	Rel          string `json:"rel"`
	ReportSHA256 string `json:"report_sha256"`
}

func (r Report) ComparedRelease() (*ComparedRelease, error) {
	data, err := json.Marshal(r.PreviousDiff)
	if err != nil {
		return nil, fmt.Errorf("encode previous_diff: %v", err)
	}
	var diff struct {
		Previous *ComparedRelease `json:"previous"`
	}
	if err = json.Unmarshal(data, &diff); err != nil {
		return nil, fmt.Errorf("decode previous_diff: %v", err)
	}
	return diff.Previous, nil
}

func readPreviousRecipe(previous *Report, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read previous release metadata: %w", err)
	}
	var metadata struct {
		Country         string                     `json:"country"`
		Rel             string                     `json:"rel"`
		RetrievalRecipe json.RawMessage            `json:"retrieval_recipe"`
		Recipes         map[string]json.RawMessage `json:"recipes"`
	}
	if err = UnmarshalStrict(data, &metadata); err != nil {
		return fmt.Errorf("decode previous release metadata: %w", err)
	}
	if metadata.Country != previous.Country || metadata.Rel != previous.Rel {
		return fmt.Errorf("previous release metadata identity differs from report")
	}
	raw := metadata.RetrievalRecipe
	if len(raw) == 0 {
		raw = metadata.Recipes["retrieval"]
	}
	var recipe retrieval.Recipe
	if err = json.Unmarshal(raw, &recipe); err != nil {
		return fmt.Errorf("decode previous retrieval recipe: %w", err)
	}
	if !recipe.Supported() {
		return fmt.Errorf("unsupported previous retrieval recipe")
	}
	if err = validatePreviousPairing(previous.Version, previous.Scorer, recipe.Version); err != nil {
		return err
	}
	if recipe.Hash() != previous.Recipes["retrieval"] {
		return fmt.Errorf("previous retrieval recipe hash differs from report")
	}
	if err = validatePreviousRecipeFields(raw, recipe); err != nil {
		return err
	}
	previous.RetrievalRecipe = raw
	return nil
}
