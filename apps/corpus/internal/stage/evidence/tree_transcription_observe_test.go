package evidence

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/detector"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func measuredTreeInput(key, source string, tree, text int, accepted bool) detectorInput {
	return detectorInput{Document: releaseDocument{Key: key, Source: "vbpl", TextSource: source}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, TreeTranscription: fetch.TreeTranscriptionV1{State: "measured", TreeTranscriptionDetails: &fetch.TreeTranscriptionDetails{TreeChars: tree, TranscriptionChars: text, TreeTextSHA256: strings.Repeat("a", 64), TranscriptionTextSHA256: strings.Repeat("b", 64), TranscriptionFileSHA256: strings.Repeat("c", 64), MainAccepted: accepted}}}}
}

func TestTreeDivergenceThresholdAndFingerprints(t *testing.T) {
	entries := []detectorInput{
		measuredTreeInput("vn:vbpl:1", "source_tree", 60, 100, true),
		measuredTreeInput("vn:vbpl:2", "source_tree", 59, 100, true),
		measuredTreeInput("vn:vbpl:3", "source_tree", 100, 59, true),
		measuredTreeInput("vn:vbpl:4", "source_tree", 61, 100, true),
		measuredTreeInput("vn:vbpl:5", "source_tree", math.MaxInt, math.MaxInt, true),
		measuredTreeInput("vn:vbpl:6", "source_tree", math.MaxInt, 1, true),
	}
	obs, err := observeTreeTranscription(entries)
	if err != nil || obs.Observed != len(entries) || len(obs.Findings) != 3 {
		t.Fatalf("obs=%+v err=%v", obs, err)
	}
	for i, want := range []struct{ key, tree, text string }{{"vn:vbpl:2", "59", "100"}, {"vn:vbpl:3", "100", "59"}, {"vn:vbpl:6", fmt.Sprint(math.MaxInt), "1"}} {
		got := obs.Findings[i]
		inputs := []string{want.tree, want.text, strings.Repeat("a", 64), strings.Repeat("b", 64)}
		if got.Subject.Type != "document" || !reflect.DeepEqual(got.Subject.Fields, []string{want.key}) || got.Discriminator != "" || got.Severity != detector.Info || !reflect.DeepEqual(got.FingerprintInputs, inputs) {
			t.Fatalf("finding=%+v", got)
		}
	}
	for _, entry := range entries {
		if !obs.ObservedCompletely(docSubject(entry.Document.Key)) {
			t.Fatalf("not covered %s", entry.Document.Key)
		}
	}
}

func TestTreeDivergenceCoverage(t *testing.T) {
	kept := measuredTreeInput("vn:vbpl:1", "source_tree", 100, 100, true)
	switched := measuredTreeInput("vn:vbpl:2", "html_transcription", 10, 100, true)
	unaccepted := measuredTreeInput("vn:vbpl:3", "source_tree", 10, 100, false)
	noPair := detectorInput{Document: releaseDocument{Key: "vn:vbpl:4", Source: "vbpl", TextSource: "source_tree"}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, TreeTranscription: fetch.TreeTranscriptionV1{State: "no_pair"}}}
	other := detectorInput{Document: releaseDocument{Key: "vn:chinhphu:5", Source: "chinhphu", TextSource: "none"}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, TreeTranscription: fetch.TreeTranscriptionV1{State: "not_applicable"}}}
	baseline := detectorInput{Document: releaseDocument{Key: "vn:vbpl:6", Source: "vbpl", Baseline: true}}
	terminal := detectorInput{Document: releaseDocument{Key: "vn:vbpl:7", Source: "vbpl"}}
	obs, err := observeTreeTranscription([]detectorInput{kept, switched, unaccepted, noPair, other, baseline, terminal})
	if err != nil || obs.Observed != 1 || obs.Excluded != 2 || obs.NotObservable != 4 || len(obs.Findings) != 0 {
		t.Fatalf("obs=%+v err=%v", obs, err)
	}
	for _, key := range []string{kept.Document.Key, switched.Document.Key, other.Document.Key} {
		if !obs.ObservedCompletely(docSubject(key)) {
			t.Errorf("not complete: %s", key)
		}
	}
	for _, key := range []string{unaccepted.Document.Key, noPair.Document.Key, baseline.Document.Key, terminal.Document.Key, "vn:vbpl:missing"} {
		if obs.ObservedCompletely(docSubject(key)) {
			t.Errorf("unexpected complete: %s", key)
		}
	}
	if obs.ObservedCompletely(Subject{Type: "provision", Fields: []string{kept.Document.Key, "x"}}) {
		t.Fatal("non-document subject marked complete")
	}
}

func TestTreeDivergenceInvalidMeasurementNoPartialResults(t *testing.T) {
	for _, mutate := range []func(*detectorInput){
		func(x *detectorInput) { x.Detector.TreeTranscription.TreeChars = 0 },
		func(x *detectorInput) { x.Detector.TreeTranscription.TranscriptionTextSHA256 = "bad" },
		func(x *detectorInput) { x.Detector.TreeTranscription.TreeTranscriptionDetails = nil },
		func(x *detectorInput) { x.Document.TextSource = "ocr" },
	} {
		valid := measuredTreeInput("vn:vbpl:1", "source_tree", 10, 100, true)
		bad := measuredTreeInput("vn:vbpl:2", "source_tree", 10, 100, true)
		mutate(&bad)
		obs, err := observeTreeTranscription([]detectorInput{valid, bad})
		if _, ok := err.(*IncompleteError); !ok || len(obs.Findings) != 0 || obs.Observed != 0 || obs.ObservedCompletely != nil {
			t.Fatalf("partial=%+v err=%v", obs, err)
		}
	}
}
