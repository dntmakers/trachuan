package build

import (
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func checkBuildExtractLineage(inv Inventory, item InventoryItem, dir, fetchHash string) error {
	if item.InputHash == nil || item.RecipeHash == nil {
		return fmt.Errorf("missing extract input or recipe hash")
	}
	contract, err := stage.ExtractContract(inv.ExtractSnapshot, *item.RecipeHash)
	if err != nil {
		return err
	}
	return fetch.CheckExtractLineageContract(dir, fetchHash, *item.InputHash, contract)
}
