package classify

import "fmt"

func (d packDir) verifyLocalDocumentCounts(state openAITrialState, items []Item) error {
	var local []Item
	for _, item := range items {
		if item.Local {
			local = append(local, item)
		}
	}
	if len(local) == 0 {
		return nil
	}
	names, err := d.names()
	if err != nil {
		return fmt.Errorf("list trial count evidence: %w", err)
	}
	read := func(name string, dst any) (bool, error) {
		raw, found, readErr := d.read(name)
		if readErr != nil || !found {
			return found, readErr
		}
		if readErr = decodePackJSON(name, raw, dst); readErr != nil {
			return false, readErr
		}
		if !packSameRecord(raw, dst) {
			return false, fmt.Errorf("invalid count record %s", name)
		}
		return true, nil
	}
	known, err := readTrialCountJournal(state, names, read)
	if err != nil {
		return err
	}
	var cache map[string]int64
	cached, err := read("counts.json", &cache)
	if err != nil {
		return err
	}
	for i, item := range local {
		if known[item.RequestHash] != item.InputTokens || cached && cache[item.RequestHash] != item.InputTokens {
			return fmt.Errorf("plan.json local document %d request_hash or counted_input_tokens differs from stored count evidence", i)
		}
	}
	return nil
}
