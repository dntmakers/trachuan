package mcpapi

import (
	"encoding/json"
	"testing"
)

func TestChangesOutput_StateAndHandleRules(t *testing.T) {
	s, err := changesOutputSchema().Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		json  string
		valid bool
	}{
		{`{"country":"vn","release":"20991110","state":"available","previous_release":"20991109","counts":{"document_added":0,"document_removed":0,"document_validity":0,"provision_validity":0,"provision_text":0,"document_key":0},"items":[]}`, true},
		{`{"country":"vn","release":"20991110","state":"available","items":[]}`, false},
		{`{"country":"vn","release":"20991110","state":"baseline","items":[]}`, true},
		{`{"country":"vn","release":"20991110","state":"baseline","items":[],"next_cursor":"x"}`, false},
		{`{"country":"vn","release":"20991110","state":"unavailable","items":[]}`, true},
		{`{"country":"vn","release":"20991110","state":"unavailable","items":[],"previous_release":"20991109"}`, false},
		{`{"country":"vn","release":"20991110","state":"unavailable","items":[{}]}`, false},
		{`{"country":"vn","release":"20991110","state":"available","previous_release":"20991109","counts":{"document_added":0,"document_removed":0,"document_validity":0,"provision_validity":0,"provision_text":1,"document_key":0},"items":[{"kind":"provision_text","key":"vn:1","anchor":"p1","change":"removed","provenance":false,"ambiguous":false,"handle":"vn:1#p1"}]}`, false},
		{`{"country":"vn","release":"20991110","state":"available","previous_release":"20991109","counts":{"document_added":0,"document_removed":0,"document_validity":0,"provision_validity":0,"provision_text":1,"document_key":0},"items":[{"kind":"provision_text","key":"vn:1","anchor":"p1","change":"changed","provenance":false,"ambiguous":true,"handle":"vn:1#p1"}]}`, false},
	}
	for _, tc := range tests {
		var v any
		if json.Unmarshal([]byte(tc.json), &v) != nil {
			t.Fatal(tc.json)
		}
		err := s.Validate(v)
		if (err == nil) != tc.valid {
			t.Errorf("valid=%v err=%v: %s", tc.valid, err, tc.json)
		}
	}
}
