package cli

import (
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"testing"
)

func TestOpenAITrialUnseededParsing(t *testing.T) {
	base := []string{"vn", "20261115_3", "--stage", "documents", "--select", "unseeded:1/4", "--model", "gpt-6.2-sol", "--effort", "medium", "--out", "out", "--cap-usd", "175"}
	if _, err := parseOpenAITrial(base); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"unseeded:0/4", "unseeded:1/65", "unseeded:5/4", "unseeded:x/2"} {
		args := append([]string(nil), base...)
		args[5] = value
		if _, err := parseOpenAITrial(args); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	args := append([]string(nil), base...)
	args[5] = "labels:file"
	if _, err := parseOpenAITrial(args); err == nil {
		t.Fatal("accepted labels cap above 40")
	}
	args = append([]string(nil), base...)
	args[len(args)-1] = "175.000001"
	if _, err := parseOpenAITrial(args); err == nil {
		t.Fatal("accepted unseeded cap above 175")
	}
}

func TestUnseededFamiliesIdentity(t *testing.T) {
	for _, tc := range []struct {
		hash      string
		selection *classify.DocumentSelection
		ok        bool
	}{
		{"a", &classify.DocumentSelection{LabelsHash: "a"}, true},
		{"a", &classify.DocumentSelection{LabelsHash: "b"}, false},
		{"a", nil, false},
	} {
		err := validateUnseededFamilies(tc.hash, classify.FamilyArtifact{Selection: tc.selection})
		if (err == nil) != tc.ok {
			t.Fatalf("selection %#v: %v", tc.selection, err)
		}
	}
}
