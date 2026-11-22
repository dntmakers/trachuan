package classify

import (
	"encoding/json"
	"fmt"
)

func validateLegacyItems(raw []byte, s Submission, pricePresent bool) error {
	prompt4 := s.Recipe.Article != nil && s.Recipe.Article.PromptVersion == 4
	historical := prompt4 && s.KeyVersion == 0 && s.PriceVersion == "" && !pricePresent
	if prompt4 && !historical {
		return fmt.Errorf("historical article prompt requires pilot versions")
	}
	if historical {
		return nil
	}
	var items []struct {
		Article map[string]json.RawMessage `json:"article_request"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("decode legacy item fields: %w", err)
	}
	for _, item := range items {
		for _, key := range []string{"document_title", "document_codes", "prompt_version"} {
			if _, ok := item.Article[key]; ok {
				return fmt.Errorf("legacy article field %s in nonhistorical intent", key)
			}
		}
		var nodes []map[string]json.RawMessage
		if b, ok := item.Article["allowed_subtrees"]; ok {
			if err := json.Unmarshal(b, &nodes); err != nil {
				return fmt.Errorf("decode legacy node fields: %w", err)
			}
		}
		for _, node := range nodes {
			for _, key := range []string{"has_provisions", "ordinal"} {
				if _, ok := node[key]; ok {
					return fmt.Errorf("legacy node field %s in nonhistorical intent", key)
				}
			}
		}
	}
	return nil
}
