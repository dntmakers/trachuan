package classify

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

func providerEvidenceBody(raw []byte) (map[string]json.RawMessage, error) {
	return batchapi.DecodeEvidenceProjection(raw, "type", "message")
}

func verifyMessageEvidence(raw []byte) error {
	fields, err := batchapi.DecodeEvidenceProjection(raw, "stop_reason", "content")
	if err != nil {
		return err
	}
	if content := fields["content"]; content != nil {
		var blocks []json.RawMessage
		if err = json.Unmarshal(content, &blocks); err != nil || blocks == nil {
			return fmt.Errorf("invalid message content")
		}
		for _, block := range blocks {
			if _, err = batchapi.DecodeEvidenceProjection(block, "type", "text"); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeMessageEvidence(raw []byte) (anthropic.Message, json.RawMessage, error) {
	var message anthropic.Message
	if err := verifyMessageEvidence(raw); err != nil {
		return message, nil, err
	}
	fields, err := batchapi.DecodeEvidenceProjection(raw, "stop_reason", "content")
	if err != nil {
		return message, nil, err
	}
	if reason := fields["stop_reason"]; len(reason) > 0 && string(reason) != "null" {
		value, readErr := evidenceString(reason)
		if readErr != nil {
			return message, nil, readErr
		}
		message.StopReason = anthropic.StopReason(value)
	}
	if content := fields["content"]; len(content) > 0 {
		var blocks []json.RawMessage
		if err = json.Unmarshal(content, &blocks); err != nil || blocks == nil {
			return message, nil, fmt.Errorf("invalid message content")
		}
		for _, rawBlock := range blocks {
			block, readErr := batchapi.DecodeEvidenceProjection(rawBlock, "type", "text")
			if readErr != nil {
				return message, nil, readErr
			}
			var kind, text string
			if v := block["type"]; len(v) > 0 && string(v) != "null" {
				if kind, err = evidenceString(v); err != nil {
					return message, nil, err
				}
			}
			if v := block["text"]; len(v) > 0 && string(v) != "null" {
				if text, err = evidenceString(v); err != nil {
					return message, nil, err
				}
			}
			message.Content = append(message.Content, anthropic.ContentBlockUnion{Type: kind, Text: text})
		}
	}
	usageFields, usageErr := batchapi.DecodeEvidenceProjection(raw, "usage")
	if usageErr != nil {
		return message, nil, nil
	}
	return message, usageFields["usage"], nil
}
