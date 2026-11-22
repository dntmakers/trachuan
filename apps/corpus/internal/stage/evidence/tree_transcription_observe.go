package evidence

import (
	"fmt"
	"math/bits"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/detector"
)

func observeTreeTranscription(inputs []detectorInput) (Observation, error) {
	var out Observation
	complete := make(map[string]bool, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		doc := input.Document
		if doc.Key == "" || doc.Source == "" || seen[doc.Key] {
			return Observation{}, &IncompleteError{Reason: "invalid or duplicate divergence document"}
		}
		seen[doc.Key] = true
		if doc.Baseline || !input.Observable {
			out.NotObservable++
			continue
		}
		if input.Detector == nil || input.Detector.Version != 1 {
			return Observation{}, &IncompleteError{Reason: fmt.Sprintf("missing detector inputs for %s", doc.Key)}
		}
		tree := input.Detector.TreeTranscription
		if doc.Source != "vbpl" {
			if tree.State != "not_applicable" || tree.TreeTranscriptionDetails != nil || doc.TextSource == "source_tree" {
				return Observation{}, &IncompleteError{Reason: fmt.Sprintf("invalid non-VBPL tree observation for %s", doc.Key)}
			}
			out.Excluded++
			complete[doc.Key] = true
			continue
		}
		switch tree.State {
		case "no_pair":
			if tree.TreeTranscriptionDetails != nil {
				return Observation{}, &IncompleteError{Reason: fmt.Sprintf("unexpected tree observation payload for %s", doc.Key)}
			}
			out.NotObservable++
			continue
		case "measured":
			if tree.TreeTranscriptionDetails == nil || tree.TreeChars <= 0 || tree.TranscriptionChars <= 0 || !detectorDigest(tree.TreeTextSHA256) || !detectorDigest(tree.TranscriptionTextSHA256) || !detectorDigest(tree.TranscriptionFileSHA256) || (doc.TextSource != "source_tree" && doc.TextSource != "html_transcription") {
				return Observation{}, &IncompleteError{Reason: fmt.Sprintf("invalid measured tree observation for %s", doc.Key)}
			}
		default:
			return Observation{}, &IncompleteError{Reason: fmt.Sprintf("invalid tree observation state for %s", doc.Key)}
		}
		if !tree.MainAccepted {
			out.NotObservable++
			continue
		}
		complete[doc.Key] = true
		if doc.TextSource == "html_transcription" {
			out.Excluded++
			continue
		}
		out.Observed++
		if treeDiverges(tree.TreeChars, tree.TranscriptionChars) {
			out.Findings = append(out.Findings, Fired{
				Subject:           Subject{Type: detector.SubjectDocument, Fields: []string{doc.Key}},
				Severity:          detector.Info,
				FingerprintInputs: []string{strconv.Itoa(tree.TreeChars), strconv.Itoa(tree.TranscriptionChars), tree.TreeTextSHA256, tree.TranscriptionTextSHA256},
			})
		}
	}
	out.ObservedCompletely = func(subject Subject) bool {
		return subject.Type == detector.SubjectDocument && len(subject.Fields) == 1 && complete[subject.Fields[0]]
	}
	return out, nil
}

func treeDiverges(tree, transcription int) bool {
	small, large := tree, transcription
	if small > large {
		small, large = large, small
	}
	smallHigh, smallLow := bits.Mul64(5, uint64(small))
	largeHigh, largeLow := bits.Mul64(3, uint64(large))
	return smallHigh < largeHigh || (smallHigh == largeHigh && smallLow < largeLow)
}
