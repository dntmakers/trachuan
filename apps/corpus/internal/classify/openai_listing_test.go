package classify

import (
	"fmt"
	"strings"
	"testing"
)

func TestOpenAIListingDiscovery(t *testing.T) {
	pair := openAIListingPair{SubmissionID: "s1", InputFileID: "f1"}
	batch := func(id, sub, file string, when int) string {
		return fmt.Sprintf(`{"object":"batch","id":%q,"created_at":%d,"endpoint":"/v1/responses","input_file_id":%q,"metadata":{"submission_id":%q},"status":"in_progress"}`, id, when, file, sub)
	}
	page := func(data, more string) string {
		return fmt.Sprintf(`{"object":"list","data":[%s],"has_more":%s}`, data, more)
	}
	capture := func(after, raw string) openAIListingCapture {
		return openAIListingCapture{Project: "proj", Fingerprint: "0123456789abcdef0123456789abcdef", After: after, Raw: []byte(raw)}
	}
	a, b := batch("b1", "s1", "f1", 150), batch("b2", "s2", "f2", 160)
	tests := []struct {
		name  string
		pages []openAIListingCapture
		pairs []openAIListingPair
		found bool
		count int
		bad   bool
	}{
		{"running", []openAIListingCapture{capture("", page(a, "false"))}, []openAIListingPair{pair}, true, 1, false},
		{"traversal", []openAIListingCapture{capture("", page(a, "true")), capture("b1", page(b, "false"))}, []openAIListingPair{pair, {SubmissionID: "s2", InputFileID: "f2"}}, true, 2, false},
		{"absent", []openAIListingCapture{capture("", page(b, "false"))}, []openAIListingPair{pair, {SubmissionID: "s2", InputFileID: "f2"}}, false, 1, false},
		{"old outside window", []openAIListingCapture{capture("", page(batch("b0", "foreign", "file", 50), "false"))}, []openAIListingPair{pair}, false, 0, false},
		{"incomplete", []openAIListingCapture{capture("", page(a, "true"))}, []openAIListingPair{pair}, false, 0, true},
		{"wrong cursor", []openAIListingCapture{capture("", page(a, "true")), capture("bad", page(b, "false"))}, []openAIListingPair{pair}, false, 0, true},
		{"duplicate", []openAIListingCapture{capture("", page(a+","+a, "false"))}, []openAIListingPair{pair}, false, 0, true},
		{"foreign blocks all", []openAIListingCapture{capture("", page(a+","+b, "false"))}, []openAIListingPair{pair}, false, 0, true},
		{"partial match", []openAIListingCapture{capture("", page(batch("b1", "s1", "other", 150), "false"))}, []openAIListingPair{pair}, false, 0, true},
		{"multiple matches", []openAIListingCapture{capture("", page(a+","+batch("b2", "s1", "f1", 151), "false"))}, []openAIListingPair{pair}, false, 0, true},
		{"omitted continuation", []openAIListingCapture{capture("", `{"data":[]}`)}, []openAIListingPair{pair}, false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifyOpenAIListing(tt.pages, "proj", "0123456789abcdef0123456789abcdef", 100, 200, pair, tt.pairs)
			if (err != nil) != tt.bad || got.TargetFound != tt.found || len(got.Candidates) != tt.count {
				t.Fatalf("got %+v err %v", got, err)
			}
		})
	}
}

func TestOpenAIListingRejectsMalformedEvidence(t *testing.T) {
	pair := openAIListingPair{SubmissionID: "s", InputFileID: "f"}
	fingerprint := "0123456789abcdef0123456789abcdef"
	good := `{"object":"batch","id":"b","created_at":150,"endpoint":"/v1/responses","input_file_id":"f","metadata":{"submission_id":"s"}}`
	cases := []string{
		`{"object":"list","data":[],"has_more":true}`,
		`{"object":"list","data":[],"has_more":false,"last_id":"b"}`,
		`{"object":"list","data":[` + good + `],"has_more":false,"first_id":"wrong"}`,
		`{"object":"list","data":[` + good + `],"has_more":false,"last_id":"wrong"}`,
		`{"object":"list","data":[{"object":"batch","id":"b","created_at":null}],"has_more":false}`,
		`{"object":"list","data":[{"object":"batch","id":"\ud800","created_at":150}],"has_more":false}`,
		`{"object":"list","data":[{"object":"batch","id":"b","created_at":150,"input_file_id":"f","endpoint":"/v1/responses"}],"has_more":false}`,
		`{"object":"list","data":[{"object":"batch","id":"b","created_at":150,"input_file_id":"f","endpoint":"/v1/chat/completions","metadata":{"submission_id":"s"}}],"has_more":false}`,
		`{"object":"list","data":[` + good + `],"has_more":false,"has_more":false}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			result, err := verifyOpenAIListing([]openAIListingCapture{{Project: "p", Fingerprint: fingerprint, Raw: []byte(raw)}}, "p", fingerprint, 100, 200, pair, []openAIListingPair{pair})
			if err == nil || len(result.Candidates) != 0 || result.TargetFound {
				t.Fatalf("got %+v %v", result, err)
			}
		})
	}
	for _, capture := range []openAIListingCapture{{Project: "foreign", Fingerprint: fingerprint, Raw: []byte(`{"object":"list","data":[],"has_more":false}`)}, {Project: "p", Fingerprint: "other", Raw: []byte(`{"object":"list","data":[],"has_more":false}`)}, {Project: "p", Fingerprint: fingerprint, After: "x", Raw: []byte(`{"object":"list","data":[],"has_more":false}`)}} {
		if _, err := verifyOpenAIListing([]openAIListingCapture{capture}, "p", fingerprint, 100, 200, pair, []openAIListingPair{pair}); err == nil {
			t.Fatal("accepted foreign capture")
		}
	}
}

func TestOpenAIListingRejectsDuplicateNonTargetPair(t *testing.T) {
	target := openAIListingPair{SubmissionID: "s1", InputFileID: "f1"}
	other := openAIListingPair{SubmissionID: "s2", InputFileID: "f2"}
	fingerprint := "0123456789abcdef0123456789abcdef"
	first := `{"object":"list","data":[{"object":"batch","id":"b1","created_at":150,"endpoint":"/v1/responses","input_file_id":"f2","metadata":{"submission_id":"s2"}}],"has_more":true}`
	second := strings.ReplaceAll(strings.ReplaceAll(first, `"b1"`, `"b2"`), `"has_more":true`, `"has_more":false`)
	captures := []openAIListingCapture{{Project: "p", Fingerprint: fingerprint, Raw: []byte(first)}, {Project: "p", Fingerprint: fingerprint, After: "b1", Raw: []byte(second)}}
	result, err := verifyOpenAIListing(captures, "p", fingerprint, 100, 200, target, []openAIListingPair{target, other})
	if err == nil || len(result.Candidates) != 0 || result.TargetFound {
		t.Fatalf("got %+v %v", result, err)
	}
}

func TestOpenAIListingRejectsDiscriminatorAndNullCursor(t *testing.T) {
	pair := openAIListingPair{SubmissionID: "s", InputFileID: "f"}
	fingerprint := "0123456789abcdef0123456789abcdef"
	valid := `{"object":"list","data":[{"object":"batch","id":"b","created_at":150,"endpoint":"/v1/responses","input_file_id":"f","metadata":{"submission_id":"s"}}],"has_more":false,"first_id":"b","last_id":"b"}`
	mutations := []struct{ name, old, new string }{
		{"missing page object", `"object":"list",`, ``}, {"wrong page object", `"object":"list"`, `"object":"other"`}, {"case page object", `"object":"list"`, `"Object":"list"`},
		{"missing batch object", `"object":"batch",`, ``}, {"wrong batch object", `"object":"batch"`, `"object":"other"`}, {"case batch object", `"object":"batch"`, `"Object":"batch"`},
		{"null first", `"first_id":"b"`, `"first_id":null`}, {"null last", `"last_id":"b"`, `"last_id":null`},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(valid, tc.old, tc.new, 1)
			result, err := verifyOpenAIListing([]openAIListingCapture{{Project: "p", Fingerprint: fingerprint, Raw: []byte(raw)}}, "p", fingerprint, 100, 200, pair, []openAIListingPair{pair})
			if err == nil || len(result.Candidates) != 0 || result.TargetFound {
				t.Fatalf("got %+v %v", result, err)
			}
		})
	}
}
