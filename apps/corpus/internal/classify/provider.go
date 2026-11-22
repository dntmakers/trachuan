package classify

import "fmt"

func submissionProvider(s Submission) (string, error) {
	if err := s.Recipe.validate(true); err != nil {
		return "", fmt.Errorf("invalid submission recipe: %w", err)
	}
	provider := s.Provider
	legacy := s.KeyVersion == 0 && s.PriceVersion == ""
	if legacy && (s.Provider != "" || s.Recipe.Provider != "") {
		return "", fmt.Errorf("historical submission must retain its original provider identity")
	}
	if provider == "" && s.Recipe.Provider == "" && (legacy || (s.KeyVersion == keyVersion && s.PriceVersion == batchPriceVersion)) {
		provider = "anthropic"
	} else if provider != s.Recipe.Provider || provider == "" {
		return "", fmt.Errorf("submission provider differs from recipe")
	}
	if !validProviderBudget(provider, s.ProjectID, s.Budget, s.BudgetMicroUSD) {
		return "", fmt.Errorf("invalid classification provider budget or project")
	}
	current := s.KeyVersion == keyVersion && ((provider == "anthropic" && s.PriceVersion == batchPriceVersion) || (provider == "openai" && s.PriceVersion == openAIBatchPriceVersion))
	if (!legacy || provider != "anthropic") && !current {
		return "", fmt.Errorf("invalid classification intent versions")
	}
	return provider, nil
}

func validProviderBudget(provider, project, budget string, amount int64) bool {
	switch provider {
	case "anthropic":
		return project == "" && amount != 0 && BudgetMicroUSD(budget) == amount
	case "openai":
		return openAIProjectValid(project) && ((budget == "openai-trial" && amount == 40000000) || (budget == "openai-full" && amount == 175000000))
	default:
		return false
	}
}
