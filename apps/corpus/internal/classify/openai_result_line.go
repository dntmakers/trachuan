package classify

import (
	"bytes"
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type openAIResultStatus string

const (
	openAIResultFinal  openAIResultStatus = "final"
	openAIResultFailed openAIResultStatus = "failed"
)

type openAIResultLine struct {
	Status     openAIResultStatus
	HTTPStatus int
	Body       json.RawMessage
	OutputText string
	ZeroCost   bool
}

func parseOpenAIResultLine(raw []byte, expectedID, stage string) (openAIResultLine, error) {
	result := openAIResultLine{Status: openAIResultFailed}
	fail := func() (openAIResultLine, error) { return openAIResultLine{}, fmt.Errorf("invalid OpenAI result line") }
	if expectedID == "" || (stage != "document" && stage != "article") {
		return fail()
	}
	line, err := batchapi.DecodeEvidenceProjection(raw, "custom_id", "response", "error")
	if err != nil {
		return fail()
	}
	id, err := evidenceString(line["custom_id"])
	if err != nil || id != expectedID {
		return fail()
	}
	response := line["response"]
	if bytes.Equal(response, []byte("null")) {
		if rawErr := line["error"]; len(rawErr) > 0 && !bytes.Equal(rawErr, []byte("null")) {
			fields, parseErr := batchapi.DecodeEvidenceProjection(rawErr, "code")
			if parseErr != nil {
				return fail()
			}
			code, codeErr := evidenceString(fields["code"])
			result.ZeroCost = codeErr == nil && code == "batch_expired"
		}
		return result, nil
	}
	if len(response) == 0 {
		return result, nil
	}
	envelope, err := batchapi.DecodeEvidenceProjection(response, "status_code", "body")
	if err != nil {
		return fail()
	}
	result.Body = append(json.RawMessage(nil), envelope["body"]...)
	code, ok := batchapi.TokenInteger(envelope["status_code"], 100)
	if ok && code <= 599 {
		result.HTTPStatus = int(code)
	}
	if !ok || code > 599 || code < 200 || code >= 300 || !bytes.Equal(line["error"], []byte("null")) {
		return result, nil
	}
	body, err := batchapi.DecodeEvidenceProjection(result.Body, "status", "error", "output", "incomplete_details")
	if err != nil {
		return result, nil
	}
	status, err := evidenceString(body["status"])
	if err != nil || status != "completed" || !bytes.Equal(body["error"], []byte("null")) || (len(body["incomplete_details"]) > 0 && !bytes.Equal(body["incomplete_details"], []byte("null"))) {
		return result, nil
	}
	text, ok := openAIOutputText(body["output"])
	if !ok || !openAIResultShape(text, stage) {
		return result, nil
	}
	result.Status, result.OutputText = openAIResultFinal, text
	return result, nil
}

func openAIOutputText(raw json.RawMessage) (string, bool) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return "", false
	}
	var text string
	count := 0
	for _, item := range items {
		fields, err := batchapi.DecodeEvidenceProjection(item, "type", "role", "status", "content", "phase")
		if err != nil {
			return "", false
		}
		kind, err := evidenceString(fields["type"])
		if err != nil {
			return "", false
		}
		if kind == "reasoning" {
			if len(fields["status"]) > 0 && !bytes.Equal(fields["status"], []byte("null")) {
				status, statusErr := evidenceString(fields["status"])
				if statusErr != nil || status != "completed" {
					return "", false
				}
			}
			continue
		}
		if kind != "message" {
			return "", false
		}
		if len(fields["phase"]) > 0 && !bytes.Equal(fields["phase"], []byte("null")) {
			phase, phaseErr := evidenceString(fields["phase"])
			if phaseErr != nil || phase != "final_answer" {
				return "", false
			}
		}
		role, err := evidenceString(fields["role"])
		if err != nil || role != "assistant" {
			return "", false
		}
		status, err := evidenceString(fields["status"])
		if err != nil || status != "completed" {
			return "", false
		}
		var content []json.RawMessage
		if json.Unmarshal(fields["content"], &content) != nil || len(content) == 0 {
			return "", false
		}
		for _, part := range content {
			block, err := batchapi.DecodeEvidenceProjection(part, "type", "text")
			if err != nil {
				return "", false
			}
			kind, err := evidenceString(block["type"])
			if err != nil || kind != "output_text" {
				return "", false
			}
			text, err = evidenceString(block["text"])
			if err != nil {
				return "", false
			}
			count++
		}
	}
	return text, count == 1
}

func openAIResultShape(text, stage string) bool {
	fields, err := readObject([]byte(text))
	if err != nil {
		return false
	}
	if stage == "document" {
		return openAICodes(fields, false)
	}
	for _, raw := range fields {
		article, parseErr := readObject(raw)
		if parseErr != nil || !openAICodes(article, true) {
			return false
		}
	}
	return true
}

func openAICodes(fields map[string]json.RawMessage, article bool) bool {
	if len(fields) != 1 || len(fields["codes"]) == 0 || fields["codes"][0] != '[' {
		return false
	}
	var codes []json.RawMessage
	if json.Unmarshal(fields["codes"], &codes) != nil {
		return false
	}
	for _, raw := range codes {
		if !article {
			if _, err := evidenceString(raw); err != nil {
				return false
			}
			continue
		}
		if !articleCodeShape(raw) {
			return false
		}
	}
	return true
}
