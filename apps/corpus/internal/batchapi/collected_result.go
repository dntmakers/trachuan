package batchapi

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func verifyCollectedResult(result Result) error {
	wire, err := DecodeEvidenceObject([]byte(result.WireJSON), "custom_id", "result")
	if err != nil {
		return err
	}
	var id string
	if len(wire["custom_id"]) == 0 || wire["custom_id"][0] != '"' || json.Unmarshal(wire["custom_id"], &id) != nil || id != result.CustomID || !bytes.Equal(wire["result"], []byte(result.RawJSON)) {
		return fmt.Errorf("provider result does not match raw line")
	}
	body, err := DecodeEvidenceObject(wire["result"], "type", "message", "error")
	if err != nil {
		return err
	}
	var kind string
	if len(body["type"]) == 0 || body["type"][0] != '"' || json.Unmarshal(body["type"], &kind) != nil || kind != result.Type {
		return fmt.Errorf("invalid provider result type")
	}
	if raw := body["message"]; raw != nil {
		if err = verifyCollectedMessage(raw); err != nil {
			return err
		}
	}
	if raw := body["error"]; raw != nil {
		envelope, decodeErr := DecodeEvidenceObject(raw, "type", "error", "request_id")
		if decodeErr != nil {
			return decodeErr
		}
		if inner := envelope["error"]; inner != nil {
			if _, err = DecodeEvidenceObject(inner, "type", "message"); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyCollectedMessage(raw []byte) error {
	fields, err := DecodeEvidenceObject(raw, "content", "stop_reason", "usage")
	if err != nil {
		return err
	}
	if stop := fields["stop_reason"]; len(stop) > 0 && string(stop) != "null" && stop[0] != '"' {
		return fmt.Errorf("invalid provider stop reason")
	}
	if usage := fields["usage"]; usage != nil {
		values, decodeErr := DecodeEvidenceObject(usage, "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "cache_creation")
		if decodeErr != nil {
			return decodeErr
		}
		for _, key := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
			if value := values[key]; value != nil {
				if _, ok := TokenInteger(value, 0); !ok {
					return fmt.Errorf("invalid provider usage")
				}
			}
		}
		if cache := values["cache_creation"]; cache != nil {
			if _, err = DecodeEvidenceObject(cache, "ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"); err != nil {
				return err
			}
		}
	}
	if content := fields["content"]; content != nil {
		var blocks []json.RawMessage
		if err = json.Unmarshal(content, &blocks); err != nil || blocks == nil {
			return fmt.Errorf("invalid provider content")
		}
		for _, rawBlock := range blocks {
			block, decodeErr := DecodeEvidenceObject(rawBlock, "type", "text")
			if decodeErr != nil {
				return decodeErr
			}
			for _, key := range []string{"type", "text"} {
				if value := block[key]; len(value) > 0 && string(value) != "null" && value[0] != '"' {
					return fmt.Errorf("invalid provider content field")
				}
			}
		}
	}
	return nil
}
