package classify

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func makeOpenAIResultLine(status int, body, itemError string) []byte {
	return []byte(fmt.Sprintf(`{"custom_id":"item-1","response":{"status_code":%d,"body":%s},"error":%s}`, status, body, itemError))
}

func openAIResultBody(status, content string) string {
	return fmt.Sprintf(`{"status":%q,"error":null,"output":[{"type":"reasoning","summary":[{"text":"not an answer"}]},{"type":"message","role":"assistant","status":"completed","content":%s}]}`, status, content)
}

func TestOpenAIResultLine_CompletionEvidence(t *testing.T) {
	base := openAIResultBody("completed", `[{"type":"output_text","text":"{\"codes\":[]}"}]`)
	cases := []struct {
		name, body string
		final      bool
	}{
		{"absent phase", base, true},
		{"null phase", strings.Replace(base, `"role":"assistant"`, `"phase":null,"role":"assistant"`, 1), true},
		{"final phase", strings.Replace(base, `"role":"assistant"`, `"phase":"final_answer","role":"assistant"`, 1), true},
		{"commentary phase", strings.Replace(base, `"role":"assistant"`, `"phase":"commentary","role":"assistant"`, 1), false},
		{"unknown phase", strings.Replace(base, `"role":"assistant"`, `"phase":"unknown","role":"assistant"`, 1), false},
		{"malformed phase", strings.Replace(base, `"role":"assistant"`, `"phase":42,"role":"assistant"`, 1), false},
		{"case phase", strings.Replace(base, `"role":"assistant"`, `"Phase":"final_answer","role":"assistant"`, 1), false},
		{"duplicate phase", strings.Replace(base, `"role":"assistant"`, `"phase":"final_answer","phase":"commentary","role":"assistant"`, 1), false},
		{"incomplete details", strings.Replace(base, `"error":null`, `"error":null,"incomplete_details":{"reason":"max_output_tokens"}`, 1), false},
		{"null details", strings.Replace(base, `"error":null`, `"error":null,"incomplete_details":null`, 1), true},
		{"incomplete reasoning", strings.Replace(base, `"type":"reasoning"`, `"type":"reasoning","status":"incomplete"`, 1), false},
		{"unfinished reasoning", strings.Replace(base, `"type":"reasoning"`, `"type":"reasoning","status":"in_progress"`, 1), false},
		{"completed reasoning", strings.Replace(base, `"type":"reasoning"`, `"type":"reasoning","status":"completed"`, 1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenAIResultLine(makeOpenAIResultLine(200, tc.body, "null"), "item-1", "document")
			if err != nil || (got.Status == openAIResultFinal) != tc.final {
				t.Fatalf("result = %+v, error = %v", got, err)
			}
			if string(got.Body) != tc.body {
				t.Fatal("body not retained")
			}
		})
	}
}

func TestOpenAIResultLine(t *testing.T) {
	text := func(s string) string {
		b, _ := json.Marshal(s)
		return fmt.Sprintf(`[{"type":"output_text","text":%s}]`, b)
	}
	document := `{"codes":["1.2"]}`
	article := `{"art_1":{"codes":[{"code":"1.2","confidence":0.9}]}}`
	cases := []struct {
		name, stage string
		line        []byte
		outcome     openAIResultStatus
		zero        bool
		candidate   string
		wantErr     bool
	}{
		{"document", "document", makeOpenAIResultLine(200, openAIResultBody("completed", text(document)), "null"), openAIResultFinal, false, document, false},
		{"article", "article", makeOpenAIResultLine(200, openAIResultBody("completed", text(article)), "null"), openAIResultFinal, false, article, false},
		{"large confidence", "article", makeOpenAIResultLine(200, openAIResultBody("completed", text(`{"art_1":{"codes":[{"code":"1.2","confidence":1e9999}]}}`)), "null"), openAIResultFinal, false, `{"art_1":{"codes":[{"code":"1.2","confidence":1e9999}]}}`, false},
		{"wrong id", "document", []byte(`{"custom_id":"other","response":null,"error":null}`), "", false, "", true},
		{"duplicate id", "document", []byte(`{"custom_id":"item-1","custom_id":"item-1","response":null,"error":null}`), "", false, "", true},
		{"duplicate status", "document", []byte(`{"custom_id":"item-1","response":{"status_code":200,"status_code":200,"body":{}},"error":null}`), "", false, "", true},
		{"http failure", "document", makeOpenAIResultLine(429, openAIResultBody("completed", text(document)), "null"), openAIResultFailed, false, "", false},
		{"refusal", "document", makeOpenAIResultLine(200, openAIResultBody("completed", `[{"type":"refusal","refusal":"secret"}]`), "null"), openAIResultFailed, false, "", false},
		{"multiple text", "document", makeOpenAIResultLine(200, openAIResultBody("completed", `[{"type":"output_text","text":"{}"},{"type":"output_text","text":"{}"}]`), "null"), openAIResultFailed, false, "", false},
		{"incomplete", "document", makeOpenAIResultLine(200, openAIResultBody("incomplete", text(document)), "null"), openAIResultFailed, false, "", false},
		{"expired", "document", []byte(`{"custom_id":"item-1","response":null,"error":{"code":"batch_expired","message":"secret"}}`), openAIResultFailed, true, "", false},
		{"other error", "document", []byte(`{"custom_id":"item-1","response":null,"error":{"code":"other","message":"secret"}}`), openAIResultFailed, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenAIResultLine(tc.line, "item-1", tc.stage)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("leaked provider text")
				}
				return
			}
			if got.Status != tc.outcome || got.ZeroCost != tc.zero || got.OutputText != tc.candidate {
				t.Fatalf("result = %+v", got)
			}
			if tc.name == "http failure" && len(got.Body) == 0 {
				t.Fatal("missing billable body")
			}
		})
	}
}
