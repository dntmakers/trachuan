package classify

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

func validID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && strings.ToLower(id) == id
}

func (r *Runner) Rebuild(ctx context.Context) error {
	unlock, err := r.Repository.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	rows, err := r.Repository.List(ctx)
	if err != nil {
		return err
	}
	existing := map[string]Submission{}
	for _, s := range rows {
		existing[s.ID] = s
	}
	var recovered []Submission
	_, err = r.verifyStore(rows, func(retained retainedStore) error {
		for _, s := range retained.intents {
			if rebuildErr := r.rebuildSubmission(&s, existing[s.ID], retained); rebuildErr != nil {
				return rebuildErr
			}
			recovered = append(recovered, s)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.SliceStable(recovered, func(i, j int) bool { return !nonterminal(recovered[i].State) && nonterminal(recovered[j].State) })
	return r.Repository.Restore(ctx, recovered)
}

type batchCanceller interface {
	Cancel(context.Context, string) error
}

func (r *Runner) Cancel(ctx context.Context, id, out string) error {
	if !validID(id) || out == "" {
		return fmt.Errorf("cancel requires a submission id and output directory")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Wait == nil {
		r.Wait = wait
	}
	canceller, ok := r.Client.(batchCanceller)
	s, err := r.transition(ctx, id, func(latest *Submission) (bool, error) {
		if latest.State == "cancelled" {
			return false, nil
		}
		if stateErr := pollingState(latest); stateErr != nil {
			return false, stateErr
		}
		if !ok {
			return false, fmt.Errorf("provider batch cancellation unavailable")
		}
		latest.State = "cancelling"
		return true, nil
	})
	if err != nil {
		return err
	}
	if s.State == "cancelled" {
		return writeFamilies(out, s)
	}
	if err = canceller.Cancel(ctx, s.BatchID); err != nil {
		return providerError(err)
	}
	return r.poll(ctx, &s, out)
}
