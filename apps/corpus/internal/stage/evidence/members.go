package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
)

func membersFrom(inv build.Inventory, identityInventoryHash string) (members, error) {
	return build.ProjectionMembers(inv, identityInventoryHash)
}

func (s *Stage) loadMembers(ctx context.Context, env stage.Env, run stage.Run, buildDir string) (members, error) {
	if s.members != nil {
		return s.members(ctx, env, run)
	}
	data, err := os.ReadFile(filepath.Join(buildDir, "build.json"))
	if err != nil {
		return members{}, fmt.Errorf("read build record: %w", err)
	}
	var record struct {
		IdentityInventoryHash string `json:"identity_inventory_hash"`
	}
	if err = json.Unmarshal(data, &record); err != nil {
		return members{}, refuse("decode build record: %v", err)
	}
	inv, err := build.LoadInventory(ctx, s.querier(env), run, "relate")
	if err != nil {
		return members{}, fmt.Errorf("load membership inventory: %w", err)
	}
	return membersFrom(inv, record.IdentityInventoryHash)
}
