package evidence

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/detector"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func ocrItem(kind string, page, ordinal int, position string) fetch.OCRCheckItem {
	item := fetch.OCRCheckItem{Kind: kind, Page: page, Ordinal: ordinal, OCRReadingSHA256: strings.Repeat("d", 64), PDFReadingSHA256: strings.Repeat("e", 64), Position: position}
	if position == "tail" {
		item.Location = &fetch.OCRLocation{TailScopeIndex: 1, StartParagraph: 3, EndParagraph: 4, StartLine: 5, EndLine: 6}
	}
	return item
}

func checkedOCRInput(key string, same bool, verdict string, missing bool, other int, items ...fetch.OCRCheckItem) detectorInput {
	if items == nil {
		items = []fetch.OCRCheckItem{}
	}
	return detectorInput{Document: releaseDocument{Key: key, Source: "vbpl", TextSource: "source_tree"}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, OCRCheck: fetch.OCRCheckObservation{State: "checked", OCRCheckDetails: &fetch.OCRCheckDetails{PDFSHA256: strings.Repeat("a", 64), EngineID: "engine", TextSHA256: strings.Repeat("b", 64), CheckerID: "checker", ResultSHA256: strings.Repeat("c", 64), SameDocument: same, Verdict: verdict, HasMissingText: missing, PageCount: 3, ParagraphCount: 20, LineCount: 25, ScopeCount: 3, OtherItems: other, Items: items}}}}
}

func checkItemSubject(key, kind, page, ordinal string) Subject {
	return Subject{Type: "check_item", Fields: []string{key, kind, page, ordinal}}
}

func TestOCRLabelObserverCoverageAndFingerprint(t *testing.T) {
	flagged := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 2, ocrItem("article_label", 1, 1, "tail"), ocrItem("document_number", 1, 1, "not_tail"), ocrItem("article_label", 2, 1, "ambiguous"), ocrItem("article_label", 2, 2, "unlocated"))
	rejected := checkedOCRInput("vn:vbpl:2", true, "rejected", true, 1, ocrItem("article_label", 1, 1, "tail"), ocrItem("document_number", 1, 1, "not_tail"))
	clean := checkedOCRInput("vn:vbpl:3", true, "clean", false, 0)
	missing := checkedOCRInput("vn:vbpl:4", true, "flagged", true, 1, ocrItem("article_label", 1, 1, "not_tail"))
	wrong := checkedOCRInput("vn:vbpl:5", false, "rejected", false, 1, ocrItem("article_label", 1, 1, "unlocated"))
	baseline := detectorInput{Document: releaseDocument{Key: "vn:vbpl:6", Baseline: true}}
	terminal := detectorInput{Document: releaseDocument{Key: "vn:vbpl:7"}}
	unavailable := detectorInput{Document: releaseDocument{Key: "vn:vbpl:8"}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, OCRCheck: fetch.OCRCheckObservation{State: "unavailable"}}}
	obs, err := observeOCRLabelItems([]detectorInput{flagged, rejected, clean, missing, wrong, baseline, terminal, unavailable})
	if err != nil || obs.Observed != 5 || obs.NotObservable != 3 || obs.Excluded != 5 || len(obs.Findings) != 2 {
		t.Fatalf("obs=%+v err=%v", obs, err)
	}
	want := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "engine", "checker", "flagged", strings.Repeat("d", 64), strings.Repeat("e", 64), "1", "3", "4"}
	first := obs.Findings[0]
	if first.Severity != detector.Review || first.Discriminator != "" || !reflect.DeepEqual(first.Subject, checkItemSubject("vn:vbpl:1", "article_label", "1", "1")) || !reflect.DeepEqual(first.FingerprintInputs, want) {
		t.Fatalf("finding=%+v", first)
	}
	if obs.Findings[1].FingerprintInputs[4] != "rejected" {
		t.Fatalf("rejected finding=%+v", obs.Findings[1])
	}
	cases := []struct {
		subject  Subject
		complete bool
	}{
		{checkItemSubject("vn:vbpl:1", "article_label", "1", "1"), true},
		{checkItemSubject("vn:vbpl:1", "document_number", "1", "1"), true},
		{checkItemSubject("vn:vbpl:1", "article_label", "2", "1"), false},
		{checkItemSubject("vn:vbpl:1", "article_label", "2", "2"), false},
		{checkItemSubject("vn:vbpl:1", "article_label", "3", "7"), true},
		{checkItemSubject("vn:vbpl:2", "document_number", "1", "1"), true},
		{checkItemSubject("vn:vbpl:2", "article_label", "3", "1"), false},
		{checkItemSubject("vn:vbpl:3", "article_label", "9", "1"), true},
		{checkItemSubject("vn:vbpl:4", "article_label", "1", "1"), true},
		{checkItemSubject("vn:vbpl:4", "article_label", "3", "1"), false},
		{checkItemSubject("vn:vbpl:5", "article_label", "1", "1"), false},
		{checkItemSubject("vn:vbpl:5", "article_label", "3", "1"), false},
		{checkItemSubject("vn:vbpl:6", "article_label", "1", "1"), false},
		{checkItemSubject("vn:vbpl:7", "article_label", "1", "1"), false},
		{checkItemSubject("vn:vbpl:8", "article_label", "1", "1"), false},
		{checkItemSubject("vn:vbpl:missing", "article_label", "1", "1"), false},
		{checkItemSubject("vn:vbpl:3", "article_label", "01", "1"), false},
		{checkItemSubject("vn:vbpl:3", "other", "1", "1"), false},
		{Subject{Type: "document", Fields: []string{"vn:vbpl:3"}}, false},
	}
	for _, tc := range cases {
		if got := obs.ObservedCompletely(tc.subject); got != tc.complete {
			t.Errorf("subject=%+v got=%v want=%v", tc.subject, got, tc.complete)
		}
	}
}

func TestOCRLabelObserverNoInventedCounts(t *testing.T) {
	input := []detectorInput{{Document: releaseDocument{Key: "vn:vbpl:1", Baseline: true}}, {Document: releaseDocument{Key: "vn:vbpl:2"}}, {Document: releaseDocument{Key: "vn:vbpl:3"}, Observable: true, Detector: &fetch.DetectorInputsV1{Version: 1, OCRCheck: fetch.OCRCheckObservation{State: "not_selected"}}}}
	obs, err := observeOCRLabelItems(input)
	if err != nil || obs.Observed != 0 || obs.NotObservable != 0 || obs.Excluded != 0 || len(obs.Findings) != 0 || obs.ObservedCompletely(checkItemSubject("vn:vbpl:3", "article_label", "1", "1")) {
		t.Fatalf("obs=%+v err=%v", obs, err)
	}
}

func TestOCRLabelObserverFingerprintChangesAndIgnoresResultHash(t *testing.T) {
	base := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, ocrItem("article_label", 1, 1, "tail"))
	observe := func(in detectorInput) []string {
		t.Helper()
		obs, err := observeOCRLabelItems([]detectorInput{in})
		if err != nil || len(obs.Findings) != 1 {
			t.Fatalf("obs=%+v err=%v", obs, err)
		}
		return obs.Findings[0].FingerprintInputs
	}
	original := observe(base)
	for _, mutate := range []func(*fetch.OCRCheckDetails){
		func(c *fetch.OCRCheckDetails) { c.PDFSHA256 = strings.Repeat("f", 64) },
		func(c *fetch.OCRCheckDetails) { c.TextSHA256 = strings.Repeat("f", 64) },
		func(c *fetch.OCRCheckDetails) { c.EngineID = "other" },
		func(c *fetch.OCRCheckDetails) { c.CheckerID = "other" },
		func(c *fetch.OCRCheckDetails) { c.Verdict = "rejected"; c.HasMissingText = true; c.OtherItems = 1 },
		func(c *fetch.OCRCheckDetails) { c.Items[0].OCRReadingSHA256 = strings.Repeat("f", 64) },
		func(c *fetch.OCRCheckDetails) { c.Items[0].PDFReadingSHA256 = strings.Repeat("f", 64) },
		func(c *fetch.OCRCheckDetails) { c.Items[0].Location.TailScopeIndex = 2 },
		func(c *fetch.OCRCheckDetails) {
			c.Items[0].Location.StartParagraph = 2
			c.Items[0].Location.StartLine = 4
		},
		func(c *fetch.OCRCheckDetails) { c.Items[0].Location.EndParagraph = 5; c.Items[0].Location.EndLine = 7 },
	} {
		changed := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, ocrItem("article_label", 1, 1, "tail"))
		mutate(changed.Detector.OCRCheck.OCRCheckDetails)
		if reflect.DeepEqual(original, observe(changed)) {
			t.Fatal("fingerprint did not change")
		}
	}
	changed := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, ocrItem("article_label", 1, 1, "tail"))
	changed.Detector.OCRCheck.ResultSHA256 = strings.Repeat("f", 64)
	if !reflect.DeepEqual(original, observe(changed)) {
		t.Fatal("result hash changed item fingerprint")
	}
}

func TestOCRLabelObserverSubjectChangesReopen(t *testing.T) {
	base := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, ocrItem("article_label", 1, 1, "tail"))
	original, err := observeOCRLabelItems([]detectorInput{base})
	if err != nil || len(original.Findings) != 1 {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	for _, items := range [][]fetch.OCRCheckItem{
		{ocrItem("document_number", 1, 1, "tail")},
		{ocrItem("article_label", 2, 1, "tail")},
		{ocrItem("article_label", 1, 1, "not_tail"), ocrItem("article_label", 1, 2, "tail")},
	} {
		changed := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, items...)
		got, observeErr := observeOCRLabelItems([]detectorInput{changed})
		if observeErr != nil || len(got.Findings) != 1 || reflect.DeepEqual(got.Findings[0].Subject, original.Findings[0].Subject) {
			t.Fatalf("changed=%+v err=%v", got, observeErr)
		}
	}
}

func TestOCRLabelObserverInvalidInputNoPartialResults(t *testing.T) {
	valid := checkedOCRInput("vn:vbpl:1", true, "flagged", false, 0, ocrItem("article_label", 1, 1, "tail"))
	invalid := checkedOCRInput("vn:vbpl:2", false, "rejected", false, 0, ocrItem("article_label", 1, 1, "tail"))
	out, err := observeOCRLabelItems([]detectorInput{valid, invalid})
	if _, ok := err.(*IncompleteError); !ok || len(out.Findings) != 0 || out.Observed != 0 || out.ObservedCompletely != nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
