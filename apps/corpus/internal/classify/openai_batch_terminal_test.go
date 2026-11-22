package classify

import (
	"strings"
	"testing"
)

func TestOpenAIBatchTerminal_Coverage(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"completed","request_counts":{"total":2,"completed":1,"failed":1},"output_file_id":"o","error_file_id":"e","in_progress_at":1}`
	identity := openAIBatchIdentity{BatchID: "b", SubmissionID: "s", InputFileID: "i"}
	batch, err := parseOpenAIBatchTerminal([]byte(raw), identity)
	if err != nil {
		t.Fatal(err)
	}
	capture := openAIBatchCapture{BatchID: "b", OutputFileID: "o", ErrorFileID: "e", Output: &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"a","response":null,"error":{"code":"other"}}`)}}, Error: &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"z","response":null,"error":{"code":"batch_expired"}}`)}}}
	if _, err := verifyOpenAIBatchCoverage(batch, capture, []string{"z", "a"}, "document"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*openAIBatchCapture)
	}{
		{"identity", func(c *openAIBatchCapture) { c.BatchID = "x" }},
		{"file identity", func(c *openAIBatchCapture) { c.OutputFileID = "x" }},
		{"missing file", func(c *openAIBatchCapture) { c.Output = nil }},
		{"incomplete file", func(c *openAIBatchCapture) { c.Output = &openAIFileCapture{} }},
		{"unknown", func(c *openAIBatchCapture) {
			c.Output = &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"x","response":null}`)}}
		}},
		{"duplicate", func(c *openAIBatchCapture) {
			c.Output = &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"z","response":null}`)}}
		}},
		{"empty nonzero", func(c *openAIBatchCapture) { c.Output = &openAIFileCapture{Complete: true} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := capture
			tc.mutate(&c)
			if _, err := verifyOpenAIBatchCoverage(batch, c, []string{"a", "z"}, "document"); err == nil {
				t.Fatal("accepted invalid capture")
			}
		})
	}
	for _, replacement := range []string{`"total":3`, `"Total":2`, `"total":null`} {
		t.Run(replacement, func(t *testing.T) {
			b, err := parseOpenAIBatchTerminal([]byte(strings.Replace(raw, `"total":2`, replacement, 1)), identity)
			if err != nil || b.CountsUsable {
				t.Fatal("accepted invalid counts")
			}
		})
	}
	for _, wrong := range []openAIBatchIdentity{{"x", "s", "i"}, {"b", "x", "i"}, {"b", "s", "x"}, {"", "s", "i"}} {
		if _, err := parseOpenAIBatchTerminal([]byte(raw), wrong); err == nil {
			t.Fatal("accepted wrong identity")
		}
	}
}

func TestOpenAIBatchTerminal_ValidationEvidenceFailClosed(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"failed","request_counts":{"total":0,"completed":0,"failed":0},"output_file_id":null,"error_file_id":null,"in_progress_at":null,"errors":{"data":[{"code":"invalid_request"}]}}`
	batch, err := parseOpenAIBatchTerminal([]byte(raw), openAIBatchIdentity{"b", "s", "i"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOpenAIBatchCoverage(batch, openAIBatchCapture{BatchID: "b"}, []string{"a"}, "document"); err == nil {
		t.Fatal("unverified validation evidence accepted")
	}
}

func TestOpenAIBatchTerminal_ZeroWork(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"failed","request_counts":{"total":0,"completed":0,"failed":0},"output_file_id":null,"error_file_id":null,"in_progress_at":null,"finalizing_at":null,"errors":{"object":"list","data":[{"code":"invalid_file","message":"private"}]},"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`
	identity := openAIBatchIdentity{"b", "s", "i"}
	batch, err := parseOpenAIBatchTerminal([]byte(raw), identity)
	if err != nil {
		t.Fatal(err)
	}
	if !isOpenAIBatchZeroWork(batch) {
		t.Fatal("valid zero-work evidence rejected")
	}
	cases := []struct{ name, old, new string }{
		{"status", `"status":"failed"`, `"status":"expired"`},
		{"processing", `"in_progress_at":null`, `"in_progress_at":1`},
		{"finalizing", `"finalizing_at":null`, `"finalizing_at":1`},
		{"output", `"output_file_id":null`, `"output_file_id":"file"`},
		{"error file", `"error_file_id":null`, `"error_file_id":"file"`},
		{"errors missing", `"errors":`, `"other_errors":`},
		{"wrong object", `"object":"list"`, `"object":"error"`},
		{"empty errors", `[{"code":"invalid_file","message":"private"}]`, `[]`},
		{"missing code", `"code":"invalid_file"`, `"other":"invalid_file"`},
		{"null code", `"code":"invalid_file"`, `"code":null`},
		{"input usage", `"input_tokens":0`, `"input_tokens":1`},
		{"output usage", `"output_tokens":0`, `"output_tokens":1`},
		{"total usage", `"total_tokens":0`, `"total_tokens":1`},
		{"cached usage", `"cached_tokens":0`, `"cached_tokens":1`},
		{"reasoning usage", `"reasoning_tokens":0`, `"reasoning_tokens":1`},
		{"malformed usage", `"input_tokens":0`, `"input_tokens":null`},
		{"missing count", `"total":0`, `"other":0`},
		{"nonzero counts", `"total":0,"completed":0`, `"total":1,"completed":1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := strings.Replace(raw, tc.old, tc.new, 1)
			b, e := parseOpenAIBatchTerminal([]byte(candidate), identity)
			if e == nil && isOpenAIBatchZeroWork(b) {
				t.Fatal("accepted contradictory evidence")
			}
		})
	}
	noUsage := strings.Replace(raw, `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`, "", 1)
	b, err := parseOpenAIBatchTerminal([]byte(noUsage), identity)
	if err != nil || !isOpenAIBatchZeroWork(b) {
		t.Fatal("absent usage rejected")
	}
}

func TestOpenAIBatchTerminal_InvalidCountsStillAllowCapture(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"cancelled","request_counts":{"total":2,"completed":1,"failed":1},"output_file_id":"o","error_file_id":"e"}`
	old := `"request_counts":{"total":2,"completed":1,"failed":1}`
	for _, counts := range []string{`"other":null`, `"request_counts":null`, `"Request_counts":{"total":2,"completed":1,"failed":1}`, `"request_counts":{"total":2,"completed":1,"failed":1},"request_counts":{"total":2,"completed":1,"failed":1}`, `"request_counts":{"Total":2,"completed":1,"failed":1}`, `"request_counts":{"total":2,"total":2,"completed":1,"failed":1}`, `"request_counts":{"total":2,"completed":1}`, `"request_counts":{"total":2,"completed":1,"failed":null}`, `"request_counts":{"total":2,"completed":1,"failed":0}`} {
		t.Run(counts, func(t *testing.T) {
			batch, err := parseOpenAIBatchTerminal([]byte(strings.Replace(raw, old, counts, 1)), openAIBatchIdentity{"b", "s", "i"})
			if err != nil || batch.OutputFileID != "o" || batch.ErrorFileID != "e" || batch.CountsUsable {
				t.Fatalf("capture rejected or counts accepted: %#v %v", batch, err)
			}
			if _, err := verifyOpenAIBatchCoverage(batch, openAIBatchCapture{BatchID: "b", OutputFileID: "o", ErrorFileID: "e"}, []string{"a"}, "document"); err == nil {
				t.Fatal("invalid counts settled")
			}
			zeroRaw := strings.Replace(strings.Replace(strings.Replace(raw, old, counts, 1), `"status":"cancelled"`, `"status":"failed"`, 1), `,"output_file_id":"o","error_file_id":"e"`, `,"errors":{"object":"list","data":[{"code":"invalid"}]}`, 1)
			zero, zeroErr := parseOpenAIBatchTerminal([]byte(zeroRaw), openAIBatchIdentity{"b", "s", "i"})
			if zeroErr != nil || isOpenAIBatchZeroWork(zero) {
				t.Fatal("invalid counts proved zero work")
			}
		})
	}
}

func TestOpenAIBatchTerminal_ZeroWorkWithoutStartField(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"failed","request_counts":{"total":0,"completed":0,"failed":0},"errors":{"object":"list","data":[{"code":"invalid_file"}]}}`
	batch, err := parseOpenAIBatchTerminal([]byte(raw), openAIBatchIdentity{"b", "s", "i"})
	if err != nil || !isOpenAIBatchZeroWork(batch) {
		t.Fatal("missing optional start rejected")
	}
}

func TestOpenAIBatchTerminal_AmbiguousUsageBlocksSettlementNotDiscovery(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"completed","request_counts":{"total":1,"completed":1,"failed":0},"output_file_id":"o"}`
	capture := openAIBatchCapture{BatchID: "b", OutputFileID: "o", Output: &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"a","response":null}`)}}}
	identity := openAIBatchIdentity{"b", "s", "i"}
	batch, err := parseOpenAIBatchTerminal([]byte(raw), identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOpenAIBatchCoverage(batch, capture, []string{"a"}, "document"); err != nil {
		t.Fatal(err)
	}
	for _, usage := range []string{`,"usage":{},"usage":{}`, `,"Usage":{}`, `,"usage":null`, `,"usage":{"input_tokens":0}`, `,"usage":{"input_tokens":-1,"output_tokens":0,"total_tokens":0}`} {
		t.Run(usage, func(t *testing.T) {
			candidate := strings.TrimSuffix(raw, "}") + usage + "}"
			b, e := parseOpenAIBatchTerminal([]byte(candidate), identity)
			if e != nil || b.OutputFileID != "o" {
				t.Fatalf("discovery failed: %v", e)
			}
			if _, coverageErr := verifyOpenAIBatchCoverage(b, capture, []string{"a"}, "document"); coverageErr == nil {
				t.Fatal("invalid usage allowed coverage")
			}
			zero := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"failed","request_counts":{"total":0,"completed":0,"failed":0},"errors":{"object":"list","data":[{"code":"invalid"}]}}`
			z, e := parseOpenAIBatchTerminal([]byte(strings.TrimSuffix(zero, "}")+usage+"}"), identity)
			if e != nil || isOpenAIBatchZeroWork(z) {
				t.Fatal("invalid usage allowed zero work")
			}
		})
	}
}

func TestOpenAIBatchTerminal_ValidUsagePreserved(t *testing.T) {
	raw := `{"id":"b","endpoint":"/v1/responses","input_file_id":"i","metadata":{"submission_id":"s"},"status":"completed","request_counts":{"total":1,"completed":1,"failed":0},"output_file_id":"o","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`
	batch, err := parseOpenAIBatchTerminal([]byte(raw), openAIBatchIdentity{"b", "s", "i"})
	if err != nil || string(batch.Usage) != `{"input_tokens":2,"output_tokens":1,"total_tokens":3}` {
		t.Fatal("valid aggregate not retained")
	}
	capture := openAIBatchCapture{BatchID: "b", OutputFileID: "o", Output: &openAIFileCapture{Complete: true, Lines: [][]byte{[]byte(`{"custom_id":"a","response":null}`)}}}
	if _, err := verifyOpenAIBatchCoverage(batch, capture, []string{"a"}, "document"); err != nil {
		t.Fatal(err)
	}
}
