package evaluation

import (
	"fmt"
	"regexp"
)

var (
	previousRelRe  = regexp.MustCompile(`^[0-9]{8}(?:_[0-9]+)?$`)
	previousHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func knownPreviousScorer(previous, current string) bool {
	switch previous {
	case current, "agent-eval/1", "agent-eval/2", "agent-eval/3", "agent-eval/4", "agent-eval/5":
		return true
	}
	return false
}

func comparablePrevious(o Options, suite Suite, r *Report) (*Report, string, error) {
	if o.PreviousReport == "" {
		return nil, "", nil
	}
	prior, hash, err := readPreviousReport(o.PreviousReport, o.PreviousRelease)
	if err != nil {
		return nil, "", err
	}
	if prior.Country != r.Country || !previousRelRe.MatchString(prior.Rel) || !previousHashRe.MatchString(prior.FixtureHashes.Development) {
		return nil, "", fmt.Errorf("previous report identity is missing or from another country")
	}
	if prior.Version < 1 || prior.Version > 5 || !knownPreviousScorer(prior.Scorer, r.Scorer) || prior.FixtureHashes.Legacy != r.FixtureHashes.Legacy || prior.FixtureHashes.OldProduct != r.FixtureHashes.OldProduct {
		return nil, "", fmt.Errorf("incompatible previous scorer or fixtures")
	}
	if r.Version == 5 {
		if err = validateCutoverPrevious(&prior, suite); err != nil {
			return nil, "", err
		}
	}
	if r.revision, err = resolveRevision(suite, &prior); err != nil {
		return nil, "", err
	}
	switch {
	case !comparableScorer(prior.Scorer, r.Scorer):
		r.PreviousDiff = map[string]any{"reason": "incompatible scorer version"}
	case (prior.CapOverride == nil) != (o.CapOverride == nil) || (prior.CapOverride != nil && o.CapOverride != nil && *prior.CapOverride != *o.CapOverride):
		r.PreviousDiff = map[string]any{"reason": "incompatible cap override"}
	case (prior.ArticleCapOverride == nil) != (o.ArticleCapOverride == nil) || (prior.ArticleCapOverride != nil && o.ArticleCapOverride != nil && *prior.ArticleCapOverride != *o.ArticleCapOverride):
		r.PreviousDiff = map[string]any{"reason": "incompatible article cap override"}
	case !sameValidityOverrides(prior.ValidityOverrides, o.ValidityOverrides):
		r.PreviousDiff = map[string]any{"reason": "incompatible validity policy"}
	case prior.carriesOverride && !o.hasValidityOverride():
		r.PreviousDiff = map[string]any{"reason": "previous report carries a research override"}
	default:
		if r.Version != 5 {
			if err = validateCutoverPrevious(&prior, suite); err != nil {
				return nil, "", err
			}
		}
		if err = validatePreviousComparisons(&prior, o.Modes); err != nil {
			return nil, "", err
		}
		return &prior, hash, nil
	}
	return nil, "", nil
}

func (o Options) hasValidityOverride() bool {
	return o.CapOverride != nil || o.ArticleCapOverride != nil || o.NonCurrentTail != nil || o.NotYetEligible
}
