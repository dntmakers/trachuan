package parse

import "fmt"

func pciPageKind(page int, detected bool) (bool, error) {
	if page < 42 || page > 372 {
		return false, fmt.Errorf("invalid page kind")
	}
	table := true
	switch page {
	case 42, 43, 65, 78, 114, 123, 138, 165, 178, 179, 214, 240, 264, 294, 338, 339, 346, 350, 351:
		table = false
	}
	if table != detected {
		return false, fmt.Errorf("page kind mismatch")
	}
	return table, nil
}
