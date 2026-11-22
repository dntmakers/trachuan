package extract

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"

	"danny.vn/trachuan/apps/corpus/internal/textparse"
	"danny.vn/trachuan/apps/corpus/internal/vbpl"
)

func partialTreeSelection(rows []provision, nodes []vbpl.Node, transcription, kept selection) (selection, []vbpl.Node, error) {
	treeText, transcriptionText, accepted, usable := partialTreeTexts(rows, transcription)
	treeChars, transcriptionChars := utf8.RuneCountInString(treeText), utf8.RuneCountInString(transcriptionText)
	observation := treeTranscriptionValue(treeText, transcriptionText, accepted, usable, transcription)
	kept.treeTranscription, transcription.treeTranscription = observation, observation
	if !usable || treeChars*2 >= transcriptionChars {
		return kept, nodes, nil
	}
	if !accepted {
		detail := fmt.Sprintf("tree_chars=%d transcription_chars=%d", treeChars, transcriptionChars)
		if kept.gap == "" {
			kept.gap, kept.reason = "tree_text_partial", detail
		} else {
			kept.extraGap, kept.extraReason = "tree_text_partial", detail
		}
		return kept, nodes, nil
	}
	transcription.tree = &treeComparison{SetAside: true, Rule: "partial_tree", TreeChars: &treeChars, TranscriptionChars: &transcriptionChars}
	return transcription, nil, nil
}

func partialTreeLengths(rows []provision, transcription selection) (int, int, bool, bool) {
	tree, text, accepted, usable := partialTreeTexts(rows, transcription)
	return utf8.RuneCountInString(tree), utf8.RuneCountInString(text), accepted, usable
}

func partialTreeTexts(rows []provision, transcription selection) (string, string, bool, bool) {
	if !validTreeIdentity(rows) {
		return "", "", false, false
	}
	rendered := renderTreeText(rows)
	var texts []string
	for _, row := range rows {
		if row.ParentNodeID == nil {
			texts = append(texts, rendered[row.NodeID])
		}
	}
	tree := collapse(strings.Join(texts, " "))
	if transcription.source != "html_transcription" || transcription.tooLarge || transcription.parseLimit {
		return tree, "", false, false
	}
	paragraphs, err := textparse.ParagraphsFromHTML(transcription.data)
	if err != nil {
		return tree, "", false, false
	}
	texts = nil
	for _, p := range paragraphs {
		texts = append(texts, p.Text)
	}
	text := collapse(strings.Join(texts, " "))
	result := textparse.Parse(paragraphs)
	accepted := false
	for _, scope := range result.Scopes {
		if scope.Kind == "main" && scope.Accepted {
			accepted = true
		}
	}
	return tree, text, accepted, true
}

func treeTranscriptionValue(tree, text string, accepted, usable bool, transcription selection) treeTranscriptionObservation {
	result := treeTranscriptionObservation{State: "no_pair"}
	if !usable || tree == "" || text == "" || !validSelectionHash(transcription.hash) || fmt.Sprintf("%x", sha256.Sum256(transcription.data)) != transcription.hash {
		return result
	}
	return treeTranscriptionObservation{
		State: "measured", TreeChars: utf8.RuneCountInString(tree), TranscriptionChars: utf8.RuneCountInString(text),
		TreeTextSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tree))), TranscriptionTextSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text))),
		TranscriptionFileSHA256: transcription.hash, MainAccepted: accepted,
	}
}
