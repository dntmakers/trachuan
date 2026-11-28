package knowledge

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func validControlLaw(id string) string {
	return "- id: \"" + id + "\"\n" +
		"  kind: control_law_link\n" +
		"  target: { key: \"vn:vbpl:1\", anchor: \"dieu-5\" }\n" +
		"  claim: { control: { code: \"nist\", version: \"1\", citation: \"AC-1\" }, relation_kind: related }\n" +
		"  evidence: { quote: \"exact text of the target provision\" }\n" +
		"  depends:\n" +
		"    contract: 1\n" +
		"    target:" + validProvision("      ") +
		"    target_own_sha256: \"" + testHash("d") + "\"\n" +
		"    control_sha256: \"" + testHash("e") + "\"\n" +
		"  reviewed: { date: \"2099-11-18\", by: \"owner\", method: \"human\" }\n"
}

func TestParseControlLaw(t *testing.T) {
	file, err := Parse("vn", []byte(validControlLaw("law-a")))
	if err != nil || file.Len() != 1 || len(file.ControlLawLinks) != 1 {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
	link := file.ControlLawLinks[0]
	if link.ID != "law-a" || link.Control != (ControlKey{"nist", "1", "AC-1"}) || link.TargetKey != "vn:vbpl:1" || link.TargetAnchor != "dieu-5" || link.RelationKind != "related" || link.Quote != "exact text of the target provision" || link.Contract != 1 || link.TargetDepends.TextSHA256 != testHash("a") || link.TargetOwnSHA256 != testHash("d") || link.ControlSHA256 != testHash("e") || link.Reviewed.By != "owner" {
		t.Fatalf("link = %+v", link)
	}
}

func TestParseControlLawRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(string) string
		want string
	}{
		"empty control code":   {replaced(`code: "nist"`, `code: ""`), "control"},
		"bad control version":  {replaced(`version: "1"`, `version: "V1"`), "control"},
		"multiline citation":   {replaced(`citation: "AC-1"`, `citation: "AC-1\nparagraph"`), "control"},
		"fragment law key":     {replaced(`key: "vn:vbpl:1"`, `key: "vn:vbpl:1#dieu-5"`), "key"},
		"free text source":     {replaced(`text_source: "source_tree"`, `text_source: "publisher paragraph"`), "text_source"},
		"relation":             {replaced("relation_kind: related", "relation_kind: cites"), "relation_kind"},
		"anchor":               {replaced(`anchor: "dieu-5"`, `anchor: "dieu-5~2"`), "anchor"},
		"control quote":        {replaced(`citation: "AC-1"`, `citation: "AC-1", quote: "text"`), "unknown field"},
		"paraphrase":           {replaced("relation_kind: related", "paraphrase: text, relation_kind: related"), "unknown field"},
		"private digest":       {replaced("    contract: 1", "    private_sha256: text\n    contract: 1"), "unknown field"},
		"rationale":            {replaced("relation_kind: related", "rationale: text, relation_kind: related"), "unknown field"},
		"missing contract":     {replaced("    contract: 1\n", ""), "contract"},
		"missing target":       {replaced("    target:"+validProvision("      "), ""), "target"},
		"missing own hash":     {replaced("    target_own_sha256: \""+testHash("d")+"\"\n", ""), "target_own_sha256"},
		"missing control hash": {replaced("    control_sha256: \""+testHash("e")+"\"\n", ""), "control_sha256"},
		"duplicate":            {func(string) string { return validControlLaw("law-a") + validControlLaw("law-b") }, "same identity"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("vn", []byte(tc.edit(validControlLaw("law-a"))))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestControlSHA256(t *testing.T) {
	root := ControlRecord{ID: ControlKey{Code: "nist", Version: "1", Citation: "A"}, ControlKind: "control", Ordinal: 1, Status: "active"}
	parent := ControlKey{Code: "nist", Version: "2", Citation: "A"}
	previous := ControlKey{Code: "nist", Version: "1", Citation: "A.1"}
	child := ControlRecord{ID: ControlKey{Code: "nist", Version: "2", Citation: "A.1"}, ControlKind: "control", Parent: &parent, Path: []string{"A"}, Ordinal: 2, Title: "Điều <&>\n", Status: "active", Supersedes: &previous}
	for _, tc := range []struct {
		record ControlRecord
		want   string
	}{
		{root, "sha256:d73fa4427cbd6bd75bd7de1b7cbb3b9f4726174d805a320d1396d7f88abc9674"},
		{child, "sha256:0dcaf092b61dc7b6709eb0b14cbc83ac456aa814843fb89c64fa4bc6a7d47e43"},
	} {
		got, err := ControlSHA256(tc.record)
		if err != nil || got != tc.want {
			t.Errorf("got %q, %v, want %q", got, err, tc.want)
		}
	}
}

func TestControlLawTextSourcesMatchSchema(t *testing.T) {
	data, err := os.ReadFile("../corpusdb/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`text_source text NOT NULL CHECK \(text_source IN \(([^)]*)\)\)`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("text_source schema check missing")
	}
	values := strings.Split(strings.ReplaceAll(string(match[1]), "'", ""), ",")
	slices.Sort(values)
	actual := slices.Clone(controlLawTextSources)
	slices.Sort(actual)
	if !slices.Equal(values, actual) {
		t.Fatalf("schema %v != validator %v", values, actual)
	}
}

func TestParseReleaseRefusesControlLaw(t *testing.T) {
	_, err := ParseRelease("vn", []byte(validControlLaw("law-a")))
	if err == nil || !strings.Contains(err.Error(), "cannot be validated against a law release yet") {
		t.Fatalf("err = %v", err)
	}
}
