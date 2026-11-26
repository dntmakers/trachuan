package build

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/internal/retrieval"
)

func (s *Stage) acceptsRecipeHash(snapshot stage.Snapshot, hash string) bool {
	return hash == s.Recipe(snapshot) || hash == s.recipeForRetrieval(snapshot, retrieval.Recipe3()) || (snapshot.ClassificationSetSHA256 == "" && hash == s.recipeForRetrieval(snapshot, retrieval.Recipe2()))
}

func (s *Stage) recordRecipeHash(snapshot stage.Snapshot, raw json.RawMessage, schemaVersion int) (string, error) {
	var recipe retrieval.Recipe
	if err := json.Unmarshal(raw, &recipe); err != nil {
		return "", fmt.Errorf("decode retrieval recipe: %w", err)
	}
	if !recipe.Supported() || (recipe.Version != 2 && recipe.Version != 3 && recipe.Version != 4) {
		return "", fmt.Errorf("unsupported retrieval recipe")
	}
	if recipe.Version == 2 {
		if snapshot.ClassificationSetSHA256 != "" {
			return "", fmt.Errorf("classification requires retrieval recipe 3")
		}
		return s.recipeForSchema(snapshot, retrieval.Recipe2(), schemaVersion), nil
	}
	if recipe.Version == 3 {
		return s.recipeForSchema(snapshot, retrieval.Recipe3(), schemaVersion), nil
	}
	return s.recipeForSchema(snapshot, retrieval.Recipe4(), schemaVersion), nil
}
