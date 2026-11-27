package releasechange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

type projectionCase struct {
	Name    string         `json:"name"`
	Input   []byte         `json:"input"`
	Refused bool           `json:"refused"`
	SHA256  string         `json:"sha256,omitempty"`
	Counts  map[string]int `json:"counts,omitempty"`
}

func projectionCaseResult(name string, input []byte) projectionCase {
	result := projectionCase{Name: name, Input: input, Refused: true}
	line, _, _ := bytes.Cut(input, []byte("\n"))
	var header struct {
		Bindings ChangesBindings `json:"bindings"`
		Baseline bool            `json:"baseline"`
	}
	if json.Unmarshal(bytes.TrimSuffix(line, []byte("\r")), &header) != nil {
		return result
	}
	previous, hash := header.Bindings.PredecessorRel, header.Bindings.PredecessorIdentitySHA256
	if previous == "" || (header.Baseline && hash != "none") || (!header.Baseline && (previous == "none" || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(hash))) {
		return result
	}
	h := releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: header.Bindings.Rel, Baseline: header.Baseline}
	if previous != "none" {
		h.Predecessor = &previous
	}
	if hash != "none" {
		bare := strings.TrimPrefix(hash, "sha256:")
		h.PredecessorIdentityHash = &bare
	}
	var canonical bytes.Buffer
	writer, err := releasechanges.NewWriter(h, &canonical)
	if err != nil {
		return result
	}
	err = ProjectStream(bytes.NewReader(input), func(row releasechanges.Row) error {
		return writer.Add(row)
	})
	if err != nil || canonical.Len() > 128*1024*1024 {
		return result
	}
	result.Refused, result.SHA256, result.Counts = false, writer.Sum(), writer.Counts()
	return result
}

func TestProjection_DifferentialCases(t *testing.T) {
	header := `{"schema":1,"bindings":{"rel":"r","dump_sha256":"d","build_artifact_sha256":"b","report_sha256":"p","predecessor_rel":"old","predecessor_identity_sha256":"sha256:` + strings.Repeat("a", 64) + `"},"baseline":false}`
	relation := func(number string) string {
		return fmt.Sprintf(`{"type":"relation","kind":"x","method":"m","source":{"key":null,"number":%q,"in_release":false,"anchor":null},"target":{"key":null,"number":"","in_release":false,"anchor":null},"change":"added","count":1}`, number)
	}
	provision := func(anchor string) string {
		return `{"type":"provision","key":"a","anchor":"` + anchor + `","change":"added","text_sha256":{"before":null,"after":null}}`
	}
	doc := func(key string) string {
		return `{"type":"document","key":"` + key + `","change":"added","cause":"membership"}`
	}
	input := func(lines ...string) []byte {
		return []byte(strings.Join(append([]string{header}, lines...), "\n") + "\n")
	}
	var cases []projectionCase
	add := func(name string, b []byte) { cases = append(cases, projectionCaseResult(name, b)) }
	add("relation_html_order", input(relation("<"), relation("A")))
	add("relation_html_reverse", input(relation("A"), relation("<")))
	add("relation_html_ampersand", input(relation("&"), relation(">")))
	add("relation_unicode_separators", input(relation(" "), relation(" ")))
	validity := func(flags string) string {
		return `{"type":"provision_validity","key":"a","anchor":"p","added":[{"status_class":"in_force","eff_from":null,"eff_to":null,"method":"text"}],"removed":[]` + flags + `}`
	}
	add("provision_validity_flags_absent", input(doc("a"), validity("")))
	add("provision_validity_flags_false", input(doc("a"), validity(`,"provenance":false,"ambiguous":false`)))
	add("astral_key", input(doc("😀")))
	escapedAstral := string(rune(92)) + "ud83d" + string(rune(92)) + "ude00"
	add("escaped_astral_key", input(doc(escapedAstral)))
	add("escaped_lone_surrogate", input(doc(`\ud800`)))
	add("escaped_key_tab", input(doc(`a\u0009b`)))
	add("escaped_item_over_budget", input(provision(strings.Repeat(escapedAstral, 350))))
	add("escaped_item_below_budget", input(provision(strings.Repeat(escapedAstral, 200))))
	add("escaped_item_boundary_low", input(provision(strings.Repeat(escapedAstral, 240))))
	add("escaped_item_boundary_high", input(provision(strings.Repeat(escapedAstral, 260))))
	longNumber := strings.ReplaceAll(relation(strings.Repeat(string(rune(0x2028)), 180000)), string(rune(92))+"u2028", string(rune(0x2028)))
	add("relation_identity_oversize", input(longNumber))
	add("invalid_bare_hash", []byte(strings.ReplaceAll(header, "sha256:", "")+"\n"))
	add("invalid_baseline_hash", []byte(strings.Replace(header, `"baseline":false`, `"baseline":true`, 1)+"\n"))
	add("invalid_no_predecessor", []byte(strings.Replace(header, `"predecessor_rel":"old"`, `"predecessor_rel":"none"`, 1)+"\n"))
	baseline := strings.Replace(header, `"baseline":false`, `"baseline":true`, 1)
	baseline = strings.Replace(baseline, `sha256:`+strings.Repeat("a", 64), "none", 1)
	add("baseline_no_rows", []byte(baseline+"\n"))
	add("baseline_with_row", []byte(baseline+"\n"+doc("a")+"\n"))
	padding := strings.Repeat(" ", 1024*1024-len(header))
	add("scanner_lf_boundary", []byte(header+padding+"\n"))
	add("scanner_crlf_boundary", []byte(header+padding+"\r\n"))
	encoded, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	golden(t, "projection_cases.json", encoded)
}
