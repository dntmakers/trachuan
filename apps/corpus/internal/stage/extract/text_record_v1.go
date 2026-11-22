package extract

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

type detectorInputsV1 = fetch.DetectorInputsV1
type treeTranscriptionV1 = fetch.TreeTranscriptionV1
type treeTranscriptionDetails = fetch.TreeTranscriptionDetails

func marshalV1TextRecord(record *textRecord, selected selection, ocrText []byte) ([]byte, error) {
	if record == nil && selected.source != "source_tree" {
		return nil, fmt.Errorf("missing extract text record")
	}
	if record != nil && record.TextSource != selected.source {
		return nil, fmt.Errorf("extract text source mismatch")
	}
	ocr, err := buildOCRCheckObservation(selected, ocrText)
	if err != nil {
		return nil, err
	}
	if selected.source == "ocr" && ocr.State != "checked" {
		return nil, fmt.Errorf("OCR source requires checked observation")
	}
	tree, err := marshalTreeTranscriptionValue(selected)
	if err != nil {
		return nil, err
	}
	detector := detectorInputsV1{Version: 1, OCRCheck: ocr, TreeTranscription: tree}
	if record == nil {
		return json.Marshal(struct {
			TextSource string           `json:"text_source"`
			Detector   detectorInputsV1 `json:"detector_inputs"`
		}{TextSource: "source_tree", Detector: detector})
	}
	return json.Marshal(struct {
		*textRecord
		Detector detectorInputsV1 `json:"detector_inputs"`
	}{textRecord: record, Detector: detector})
}

func marshalTreeTranscriptionValue(selected selection) (treeTranscriptionV1, error) {
	observation := selected.treeTranscription
	switch observation.State {
	case "not_applicable", "no_pair":
		return treeTranscriptionV1{State: observation.State}, nil
	case "measured":
		if (selected.source != "source_tree" && selected.source != "html_transcription") || observation.TreeChars <= 0 || observation.TranscriptionChars <= 0 || !validSelectionHash(observation.TreeTextSHA256) || !validSelectionHash(observation.TranscriptionTextSHA256) || !validSelectionHash(observation.TranscriptionFileSHA256) {
			return treeTranscriptionV1{}, fmt.Errorf("invalid measured tree transcription")
		}
		return treeTranscriptionV1{State: "measured", TreeTranscriptionDetails: &treeTranscriptionDetails{
			TreeChars: observation.TreeChars, TranscriptionChars: observation.TranscriptionChars,
			TreeTextSHA256: observation.TreeTextSHA256, TranscriptionTextSHA256: observation.TranscriptionTextSHA256,
			TranscriptionFileSHA256: observation.TranscriptionFileSHA256, MainAccepted: observation.MainAccepted,
		}}, nil
	default:
		return treeTranscriptionV1{}, fmt.Errorf("invalid tree transcription state")
	}
}
