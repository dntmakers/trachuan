package evaluation

import "fmt"

func validatePreviousPairing(version int, scorer string, recipe int) error {
	switch version {
	case 1:
		if (scorer == "agent-eval/1" || scorer == "agent-eval/2" || scorer == "agent-eval/3" || scorer == "agent-eval/4") && (recipe == 0 || recipe == 2 || recipe == 3) {
			return nil
		}
	case 2:
		if scorer == "agent-eval/5" && (recipe == 2 || recipe == 3) {
			return nil
		}
	case 3, 4:
		if scorer == "agent-eval/6" && (recipe == 2 || recipe == 3) {
			return nil
		}
	case 5:
		if scorer == "agent-eval/6" && recipe == 4 {
			return nil
		}
	}
	return fmt.Errorf("invalid previous report version, scorer or retrieval recipe pairing")
}
