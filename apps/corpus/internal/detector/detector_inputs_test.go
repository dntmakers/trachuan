package detector

import (
	"reflect"
	"testing"
)

func TestDetectorInputsSpecs(t *testing.T) {
	for _, tc := range []struct {
		id, subject string
		severity    Severity
		fingerprint []string
	}{
		{"ocr_label_item_outside_labels", "check_item", Review, []string{"pdf_sha256", "text_sha256", "engine_id", "checker_id", "verdict", "ocr_reading_sha256", "pdf_reading_sha256", "tail_scope_index", "start_paragraph", "end_paragraph"}},
		{"tree_transcription_divergence", "document", Info, []string{"tree_chars", "transcription_chars", "tree_text_sha256", "transcription_text_sha256"}},
	} {
		got, ok := Lookup(tc.id)
		if !ok || got.Version != 1 || got.Semantics != 1 || got.Subject != tc.subject || got.Severity != tc.severity || got.Discriminator != DiscriminatorNone || got.Completeness == "" || !reflect.DeepEqual(got.Fingerprint, tc.fingerprint) {
			t.Errorf("id=%s spec=%+v ok=%v", tc.id, got, ok)
		}
	}
}

func TestDetectorInputsLegacyHash(t *testing.T) {
	var legacy []Detector
	for _, spec := range All() {
		if spec.ID != OCRLabelItemOutsideLabels && spec.ID != TreeTranscriptionDivergence {
			legacy = append(legacy, spec)
		}
	}
	if got := hashOf(legacy); got != LegacyHash() {
		t.Fatalf("legacy hash=%s want=%s", got, LegacyHash())
	}
}
