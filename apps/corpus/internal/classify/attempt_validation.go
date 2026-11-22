package classify

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

func responseText(m anthropic.Message) string {
	var text strings.Builder
	for _, v := range m.Content {
		if v.Type == "text" {
			text.WriteString(v.Text)
		}
	}
	return text.String()
}

func arrayOf(raw json.RawMessage, valid func(json.RawMessage) bool) bool {
	if len(raw) == 0 || raw[0] != '[' {
		return false
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return false
	}
	for _, entry := range entries {
		if !valid(entry) {
			return false
		}
	}
	return true
}

func jsonString(raw json.RawMessage) bool { return len(raw) > 0 && raw[0] == '"' }

func finalResponse(m anthropic.Message, article bool) bool {
	if m.StopReason != "end_turn" {
		return false
	}
	text := []byte(responseText(m))
	if !article {
		obj, err := closedObject(text, []string{"codes"})
		return err == nil && arrayOf(obj["codes"], jsonString)
	}
	anchors, err := readObject(text)
	if err != nil {
		return false
	}
	for _, raw := range anchors {
		obj, parseErr := closedObject(raw, []string{"codes"})
		if parseErr != nil || !arrayOf(obj["codes"], articleCodeShape) {
			return false
		}
	}
	return true
}

func articleCodeShape(raw json.RawMessage) bool {
	obj, err := closedObject(raw, []string{"code", "confidence"})
	if err != nil || !jsonString(obj["code"]) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(obj["confidence"]))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return false
	}
	_, ok := v.(json.Number)
	return ok
}

func resultOutcome(s Submission, item Item, m anthropic.Message, final bool) Outcome {
	var stop, text string
	if final {
		stop = string(m.StopReason)
		text = responseText(m)
	}
	out := Validate(stop, text, s.Recipe.F, s.Families)
	if item.Article != nil {
		out.Articles, out.DroppedAnchors = ValidateArticles(stop, text, *item.Article, item.Article.Nodes)
	}
	return out
}
