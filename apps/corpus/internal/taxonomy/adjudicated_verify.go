package taxonomy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

const ConsensusModel = "gpt-6.2-sol"

func consensusPins() []adjudicatedPin {
	return []adjudicatedPin{
		{"multi-family", "consensus", "8a5a76ceb5cec313b0fba4e4806cb59537081c4b51dd3d966031a066a56d13fc"},
		{"post-2023", "consensus", "ac53b485991c41e120bece309593d0c7d3eccbc0b0b5b66c8ac89521870eeb0b"},
	}
}

type adjudicatedPin struct{ slice, provider, hash string }

func adjudicatedPins() []adjudicatedPin {
	return []adjudicatedPin{
		{"seeded", "package", "3b7e9be6992cb20487e31037c6ab3f0f38cc7c1091b1249a9c37cea16599d6a5"},
		{"multi-family", "openai", "e46dc8ad6ec74fd341b0feeac5d7fbd4e5c7d2881fb758ce2f64c8b575ab3b04"},
		{"post-2023", "openai", "8d8d7ce12dc4a96338dc8553d56e4d8e9c5ccb0dfe25e8a1dc0c040918f4ce3c"},
		{"multi-family", "anthropic", "88e059257a0074d6ea3f59e5fc6e514ccc1e9dc9aaa411e2fb78782774765540"},
		{"post-2023", "anthropic", "aa18a51278bc8fec66bd1649ad1faf67a7a84f1098eeedb1fc95cf9ec71eb5db"},
	}
}

func VerifyAdjudicated(classifierProvider string, data []byte) (string, string, FrozenLabels, error) {
	slice, provider, labels, err := VerifyScoringInput(classifierProvider, data)
	if err == nil && provider == "package" {
		return "", "", FrozenLabels{}, fmt.Errorf("seeded input is not adjudicated")
	}
	return slice, provider, labels, err
}

func VerifyScoringInput(classifierProvider string, data []byte) (string, string, FrozenLabels, error) {
	return VerifyClassifierScoringInput(classifierProvider, "", data)
}

func VerifyClassifierScoringInput(classifierProvider, model string, data []byte) (string, string, FrozenLabels, error) {
	var labels FrozenLabels
	if classifierProvider != "openai" && classifierProvider != "anthropic" {
		return "", "", labels, fmt.Errorf("unknown classifier provider")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range append(adjudicatedPins(), consensusPins()...) {
		if pin.hash != hash {
			continue
		}
		if err := json.Unmarshal(data, &labels); err != nil {
			return "", "", FrozenLabels{}, fmt.Errorf("decode adjudicated labels: %w", err)
		}
		if pin.provider == "package" {
			if labels.DrawRule != DrawRuleVersion {
				return "", "", FrozenLabels{}, fmt.Errorf("inconsistent seeded provenance")
			}
			return pin.slice, pin.provider, labels, nil
		}
		provider := labels.Provider
		if provider == "" && labels.Labeler == "gpt-6-astra" {
			provider = "openai"
		}
		if provider != pin.provider || labels.DrawRule != AdjudicatedDrawRule {
			return "", "", FrozenLabels{}, fmt.Errorf("inconsistent adjudicated provenance")
		}
		if provider == "consensus" {
			if classifierProvider != "openai" || model != ConsensusModel {
				return "", "", FrozenLabels{}, fmt.Errorf("consensus labels require openai/%s", ConsensusModel)
			}
			if err := verifyConsensusProvenance(pin.slice, labels); err != nil {
				return "", "", FrozenLabels{}, err
			}
			labels.verifiedConsensus = true
		}
		if provider == classifierProvider {
			return "", "", FrozenLabels{}, fmt.Errorf("classifier and labeler providers must differ")
		}
		return pin.slice, provider, labels, nil
	}
	return "", "", labels, fmt.Errorf("unpinned adjudicated labels")
}

func verifyConsensusProvenance(slice string, labels FrozenLabels) error {
	for _, pin := range adjudicatedPins() {
		if pin.slice != slice || pin.provider != "anthropic" || pin.hash != labels.BaseLabelsSHA256 {
			continue
		}
		source, err := SourceListSHA256(labels.Articles)
		if err != nil || source != labels.SourceListSHA256 || labels.Labeler != "gpt-6.2-astra+claude-fable-5-2" {
			return fmt.Errorf("inconsistent consensus provenance")
		}
		return nil
	}
	return fmt.Errorf("incorrect consensus base pin for %s", slice)
}
