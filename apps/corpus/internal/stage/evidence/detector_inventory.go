package evidence

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type detectorInventoryItem struct {
	Stage          string  `json:"stage"`
	ItemKey        string  `json:"item_key"`
	State          string  `json:"state"`
	InputHash      *string `json:"input_hash"`
	RecipeHash     *string `json:"recipe_hash"`
	ArtifactSHA256 *string `json:"artifact_sha256"`
}

type detectorInventory struct {
	Version      int                     `json:"version"`
	Country      string                  `json:"country"`
	ExpectedKeys []string                `json:"expected_keys"`
	Items        []detectorInventoryItem `json:"items"`
	Gaps         []gapRow                `json:"gaps"`
}

func detectorDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func decodeDetectorInventory(data []byte, country string) (detectorInventory, error) {
	var inv detectorInventory
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := uniqueJSON(decoder, 0); err != nil {
		return inv, fmt.Errorf("invalid inventory: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return inv, fmt.Errorf("trailing inventory data")
	}
	if err := json.Unmarshal(data, &inv); err != nil {
		return inv, fmt.Errorf("decode inventory: %w", err)
	}
	if (inv.Version != 0 && inv.Version != 2) || inv.Country != country || inv.ExpectedKeys == nil || inv.Items == nil || inv.Gaps == nil {
		return inv, fmt.Errorf("invalid inventory header")
	}
	return inv, nil
}

func uniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("inventory nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			name, valid := key.(string)
			if !valid || seen[name] {
				return fmt.Errorf("duplicate or invalid inventory field")
			}
			seen[name] = true
			if err = uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid inventory delimiter")
	}
	_, err = decoder.Token()
	return err
}
