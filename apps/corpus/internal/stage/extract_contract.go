package stage

import (
	"encoding/hex"
	"fmt"
	"strings"
)

func ExtractContract(snapshot Snapshot, verifiedRecipe string) (string, error) {
	switch snapshot.ExtractRecordContract {
	case ExtractRecordLegacy, ExtractRecordV1:
	default:
		return "", fmt.Errorf("missing or unknown extract record contract")
	}
	if len(verifiedRecipe) != 64 || strings.ToLower(verifiedRecipe) != verifiedRecipe {
		return "", fmt.Errorf("invalid extract recipe hash")
	}
	if _, err := hex.DecodeString(verifiedRecipe); err != nil {
		return "", fmt.Errorf("invalid extract recipe hash: %w", err)
	}
	if snapshot.Recipes["extract"] != verifiedRecipe {
		return "", fmt.Errorf("extract recipe does not match frozen snapshot")
	}
	return snapshot.ExtractRecordContract, nil
}
