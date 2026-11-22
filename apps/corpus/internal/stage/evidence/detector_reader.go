package evidence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

type detectorInput struct {
	Document   releaseDocument
	Observable bool
	Detector   *fetch.DetectorInputsV1
}

func detectorIncomplete(format string, args ...any) error {
	return &IncompleteError{Reason: fmt.Sprintf(format, args...)}
}

func loadDetectorInputs(ctx context.Context, in Inputs, rel releaseData, extractRecipe string) ([]detectorInput, error) {
	if rel == nil || !detectorDigest(extractRecipe) {
		return nil, detectorIncomplete("missing release or frozen extract recipe")
	}
	buildDir, buildHash := in.BuildDir, in.BuildSHA256
	if buildDir == "" && in.Build != nil {
		var err error
		buildDir, buildHash, err = in.Build()
		if err != nil {
			return nil, detectorIncomplete("locate committed build: %v", err)
		}
	}
	if buildDir == "" || !detectorDigest(buildHash) {
		return nil, detectorIncomplete("missing committed build identity")
	}
	data, err := artifact.ReadVerifiedFile(buildDir, "inputs.json", buildHash, 256<<20)
	if err != nil {
		return nil, detectorIncomplete("build inventory failed verification: %v", err)
	}
	inv, err := decodeDetectorInventory(data, in.Run.Country)
	if err != nil {
		return nil, detectorIncomplete("%v", err)
	}
	expected := make(map[string]bool, len(inv.ExpectedKeys))
	for _, key := range inv.ExpectedKeys {
		if key == "" || expected[key] {
			return nil, detectorIncomplete("ambiguous expected key %q", key)
		}
		expected[key] = true
	}
	items := map[string]detectorInventoryItem{}
	for _, item := range inv.Items {
		if item.Stage != "fetch" && item.Stage != "extract" {
			continue
		}
		key := item.Stage + "\x00" + item.ItemKey
		if item.ItemKey == "" {
			return nil, detectorIncomplete("empty inventory item key")
		}
		if _, exists := items[key]; exists {
			return nil, detectorIncomplete("duplicate %s item %s", item.Stage, item.ItemKey)
		}
		items[key] = item
	}
	gaps := map[string]bool{}
	for _, gap := range inv.Gaps {
		if (gap.Stage == "fetch" || gap.Stage == "extract") && gap.Kind != "" {
			gaps[gap.Stage+"\x00"+gap.ItemKey] = true
		}
	}
	seen := map[string]bool{}
	var out []detectorInput
	err = rel.Documents(ctx, func(doc releaseDocument) error {
		parts := strings.SplitN(doc.Key, ":", 3)
		if len(parts) != 3 || parts[0] != in.Run.Country || parts[1] != doc.Source || parts[2] == "" || seen[doc.Key] {
			return detectorIncomplete("ambiguous release document %q", doc.Key)
		}
		seen[doc.Key] = true
		result := detectorInput{Document: doc}
		if doc.Baseline {
			out = append(out, result)
			return nil
		}
		if !expected[doc.Key] {
			return detectorIncomplete("document %s is not in committed inventory", doc.Key)
		}
		fetchItem, ok := items["fetch\x00"+doc.Key]
		if !ok {
			return detectorIncomplete("document %s has no fetch item", doc.Key)
		}
		extractItem, hasExtract := items["extract\x00"+doc.Key]
		if fetchItem.State == "gap" {
			if !gaps["fetch\x00"+doc.Key] || fetchItem.ArtifactSHA256 != nil || doc.ExtractSHA256 != "" || doc.TextSource != "none" || (hasExtract && extractItem.State != "gap") {
				return detectorIncomplete("conflicting fetch gap for %s", doc.Key)
			}
			out = append(out, result)
			return nil
		}
		if fetchItem.State != "done" || fetchItem.ArtifactSHA256 == nil || !detectorDigest(*fetchItem.ArtifactSHA256) || !hasExtract {
			return detectorIncomplete("invalid fetch or missing extract item for %s", doc.Key)
		}
		if extractItem.State == "gap" {
			if !gaps["extract\x00"+doc.Key] || extractItem.ArtifactSHA256 != nil || doc.ExtractSHA256 != "" || doc.TextSource != "none" {
				return detectorIncomplete("conflicting extract gap for %s", doc.Key)
			}
			out = append(out, result)
			return nil
		}
		if extractItem.State != "done" || extractItem.RecipeHash == nil || *extractItem.RecipeHash != extractRecipe || extractItem.InputHash == nil || !detectorDigest(*extractItem.InputHash) || extractItem.ArtifactSHA256 == nil || !detectorDigest(*extractItem.ArtifactSHA256) || doc.ExtractSHA256 != *extractItem.ArtifactSHA256 {
			return detectorIncomplete("extract identity mismatch for %s", doc.Key)
		}
		if in.ArtifactDir == nil {
			return detectorIncomplete("extract locator unavailable for %s", doc.Key)
		}
		dir, found, locateErr := in.ArtifactDir("extract", doc.Key)
		if locateErr != nil || !found || dir == "" {
			return detectorIncomplete("extract artifact unavailable for %s", doc.Key)
		}
		text, readErr := artifact.ReadVerifiedFile(dir, "text.json", doc.ExtractSHA256, 256<<20)
		if readErr != nil {
			return detectorIncomplete("extract artifact failed verification for %s: %v", doc.Key, readErr)
		}
		if lineageErr := fetch.CheckExtractLineageV1Bytes(text, *fetchItem.ArtifactSHA256, *extractItem.InputHash); lineageErr != nil {
			return detectorIncomplete("extract lineage mismatch for %s: %v", doc.Key, lineageErr)
		}
		detector, decodeErr := fetch.DecodeDetectorTextV1(text, doc.TextSource)
		if decodeErr != nil {
			return detectorIncomplete("invalid extract record for %s: %v", doc.Key, decodeErr)
		}
		if (doc.Source == "vbpl" && detector.TreeTranscription.State == "not_applicable") || (doc.Source != "vbpl" && (detector.TreeTranscription.State != "not_applicable" || doc.TextSource == "source_tree")) {
			return detectorIncomplete("tree observation and document source disagree for %s", doc.Key)
		}
		result.Observable, result.Detector = true, &detector
		out = append(out, result)
		return nil
	})
	if err != nil {
		var incomplete *IncompleteError
		if errors.As(err, &incomplete) {
			return nil, incomplete
		}
		return nil, detectorIncomplete("read release documents: %v", err)
	}
	return out, nil
}
