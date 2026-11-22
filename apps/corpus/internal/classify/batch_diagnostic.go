package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func observedBatchIdentity(raw []byte) (string, bool, error) {
	if !json.Valid(raw) {
		return "", false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return "", false, nil
	}
	ids := map[string]bool{}
	canonical := ""
	hasStatus, hasBatchType, hasError, hasNonBatchType := false, false, false, false
	for dec.More() {
		token, readErr := dec.Token()
		if readErr != nil {
			return "", false, readErr
		}
		key, ok := token.(string)
		if !ok {
			return "", false, fmt.Errorf("invalid provider response field")
		}
		var value json.RawMessage
		if readErr = dec.Decode(&value); readErr != nil {
			return "", false, readErr
		}
		switch {
		case strings.EqualFold(key, "id"):
			var id string
			if len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &id) == nil && id != "" {
				ids[id] = true
				if key == "id" {
					canonical = id
				}
			}
		case strings.EqualFold(key, "type"):
			var kind string
			if json.Unmarshal(value, &kind) == nil {
				if key == "type" && kind == "message_batch" {
					hasBatchType = true
				}
				if key == "type" && kind != "" && kind != "message_batch" && kind != "error" {
					hasNonBatchType = true
				}
				if kind == "error" {
					hasError = true
				}
			}
		case strings.EqualFold(key, "error"):
			if string(bytes.TrimSpace(value)) != "null" {
				hasError = true
			}
		case key == "processing_status":
			var status string
			if json.Unmarshal(value, &status) == nil && (status == "in_progress" || status == "canceling" || status == "ended") {
				hasStatus = true
			}
		}
	}
	if hasError || hasNonBatchType {
		return "", false, nil
	}
	if len(ids) > 1 {
		return "", false, fmt.Errorf("contradictory provider batch identities")
	}
	if canonical == "" {
		return "", false, nil
	}
	if !hasBatchType && !hasStatus {
		return canonical, false, nil
	}
	return canonical, true, nil
}
