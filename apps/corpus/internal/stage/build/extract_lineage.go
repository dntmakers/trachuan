package build

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func checkBuildExtractLineage(inv Inventory, item InventoryItem, dir, fetchHash string) error {
	if item.InputHash == nil || item.RecipeHash == nil {
		return fmt.Errorf("missing extract input or recipe hash")
	}
	if err := checkExtractArtifactAuthority(item, inv.ExtractSnapshot); err != nil {
		return err
	}
	contract, err := stage.ExtractContract(inv.ExtractSnapshot, *item.RecipeHash)
	if err != nil {
		return err
	}
	return fetch.CheckExtractLineageContract(dir, fetchHash, *item.InputHash, contract)
}

type extractArtifactAuthority struct {
	Stage      *string `json:"artifact_stage"`
	ItemKey    *string `json:"artifact_item_key"`
	InputHash  *string `json:"artifact_input_hash"`
	RecipeHash *string `json:"artifact_recipe_hash"`
	Path       *string `json:"path"`
}

func decodeInventoryItem(data json.RawMessage, snapshot stage.Snapshot) (InventoryItem, error) {
	var item InventoryItem
	var authority extractArtifactAuthority
	if err := json.Unmarshal(data, &item); err != nil {
		return item, fmt.Errorf("decode inventory item: %w", err)
	}
	if err := json.Unmarshal(data, &authority); err != nil {
		return item, fmt.Errorf("decode artifact authority: %w", err)
	}
	item.Path = authority.Path
	item.authority = &authority
	if err := checkExtractArtifactAuthority(item, snapshot); err != nil {
		return item, err
	}
	return item, nil
}

func checkExtractArtifactAuthority(item InventoryItem, snapshot stage.Snapshot) error {
	if item.Stage != "extract" || item.ArtifactSHA256 == nil {
		return nil
	}
	a := item.authority
	if a == nil || a.Stage == nil || *a.Stage != item.Stage || a.ItemKey == nil || *a.ItemKey != item.ItemKey || a.InputHash == nil || item.InputHash == nil || *a.InputHash != *item.InputHash || a.RecipeHash == nil || item.RecipeHash == nil || *a.RecipeHash != *item.RecipeHash {
		return fmt.Errorf("extract artifact authority mismatch for %q", item.ItemKey)
	}
	_, err := stage.ExtractContract(snapshot, *item.RecipeHash)
	return err
}
