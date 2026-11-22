package extract

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTreeTranscriptionObservationMeasured(t *testing.T) {
	for _, tc := range []struct {
		label, body string
		accepted    bool
	}{
		{"Điều 1.", "accepted", true},
		{"Điều 2.", "rejected", false},
	} {
		html := "<p>" + tc.label + "</p><p>" + strings.Repeat("đất lúa ", 60) + "</p>"
		fileHash := fmt.Sprintf("%x", sha256.Sum256([]byte(html)))
		selected := selection{source: "html_transcription", data: []byte(html), hash: fileHash}
		rows := []provision{{NodeID: "a", Text: "cây   lúa"}}
		got, _, err := partialTreeSelection(rows, nil, selected, selection{source: "source_tree"})
		if err != nil {
			t.Fatal(err)
		}
		obs := got.treeTranscription
		text := tc.label + " " + strings.TrimSpace(strings.Repeat("đất lúa ", 60))
		if obs.State != "measured" || obs.TreeChars != utf8.RuneCountInString("cây lúa") || obs.TranscriptionChars != utf8.RuneCountInString(text) || obs.TreeTextSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("cây lúa"))) || obs.TranscriptionTextSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) || obs.TranscriptionFileSHA256 != fileHash || obs.MainAccepted != tc.accepted {
			t.Fatalf("case=%s observation=%+v source=%s", tc.body, obs, got.source)
		}
		if tc.accepted && got.source != "html_transcription" {
			t.Fatalf("did not preserve observation on transcription: %+v", got)
		}
	}
}

func TestTreeTranscriptionObservationNoPair(t *testing.T) {
	for _, tc := range []struct {
		rows     []provision
		selected selection
	}{
		{[]provision{{NodeID: "a", Text: ""}}, selection{source: "html_transcription", data: []byte("<p>Điều 1.</p>"), hash: strings.Repeat("a", 64)}},
		{[]provision{{NodeID: "a", Text: "tree"}}, selection{source: "none"}},
		{[]provision{{NodeID: "a", Text: "tree"}}, selection{source: "html_transcription", tooLarge: true}},
		{[]provision{{NodeID: "a", Text: "tree"}}, selection{source: "html_transcription", parseLimit: true}},
	} {
		got, _, err := partialTreeSelection(tc.rows, nil, tc.selected, selection{source: "source_tree"})
		if err != nil || got.treeTranscription.State != "no_pair" {
			t.Fatalf("observation=%+v err=%v", got.treeTranscription, err)
		}
	}
}

func TestTreeTranscriptionObservationChoice(t *testing.T) {
	html := partialHTML()
	got, _, err := choose(context.Background(), transcriptionBundle(t, partialTree, html), "vn:vbpl:1")
	if err != nil || got.source != "html_transcription" || got.treeTranscription.State != "measured" || got.treeTranscription.TranscriptionFileSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(html))) {
		t.Fatalf("selection=%+v err=%v", got, err)
	}
	got, _, err = choose(context.Background(), headingBundle(t, `{"success":true,"data":[]}`, false), "vn:vbpl:1")
	if err != nil || got.treeTranscription.State != "no_pair" {
		t.Fatalf("selection=%+v err=%v", got, err)
	}
}

func TestTreeTranscriptionObservationFrozenDefaults(t *testing.T) {
	for _, tc := range []struct{ key, want string }{{"vn:vbpl:1", "no_pair"}, {"vn:chinhphu:1", "not_applicable"}} {
		got, _, err := materializeFrozen(context.Background(), t.TempDir(), tc.key, selection{source: "none", gap: "needs_ocr", consulted: true})
		if err != nil || got.treeTranscription.State != tc.want {
			t.Fatalf("key=%s got=%+v err=%v", tc.key, got.treeTranscription, err)
		}
	}
}

func TestTreeTranscriptionObservationInvalidFileDigest(t *testing.T) {
	html := []byte("<p>Điều 1.</p><p>" + strings.Repeat("body ", 100) + "</p>")
	selected := selection{source: "html_transcription", data: html, hash: strings.Repeat("a", 64)}
	got, _, err := partialTreeSelection([]provision{{NodeID: "a", Text: "tree"}}, nil, selected, selection{source: "source_tree"})
	if err != nil || got.treeTranscription.State != "no_pair" {
		t.Fatalf("observation=%+v err=%v", got.treeTranscription, err)
	}
}
