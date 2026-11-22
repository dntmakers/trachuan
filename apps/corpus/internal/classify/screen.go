package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"time"
)

func draw(docs []Document, n int, seed int64) ([]Document, error) {
	if len(docs) < n {
		return nil, fmt.Errorf("requested %d documents, only %d eligible", n, len(docs))
	}
	all := append([]Document(nil), docs...)
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	for i := 1; i < len(all); i++ {
		if all[i].Key == all[i-1].Key {
			return nil, fmt.Errorf("duplicate eligible document")
		}
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all[:n], nil
}

func screenHash(o Options) string {
	if o.article != nil {
		return articleWorkHash(o)
	}
	if o.Selection != nil {
		return documentSelectionHash(o)
	}
	b, _ := json.Marshal(struct {
		RunID            int64
		Country, Release string
		Documents        int
		Seed             int64
		RecipeHash       string
	}{o.RunID, o.Country, o.Release, o.Documents, o.Seed, o.Recipe.Hash()})
	return digest(b)
}

func legacyScreenHash(o Options) string {
	if o.Recipe.Provider != "anthropic" {
		return ""
	}
	o.Recipe.Provider = ""
	if o.Recipe.Article != nil {
		article := *o.Recipe.Article
		o.Recipe.Article = &article
		article.SourceRecipeHash = o.Recipe.documentRecipe().Hash()
	}
	return screenHash(o)
}

func matchesScreen(s Submission, current, legacy string) bool {
	return s.ScreenHash == current || (legacy != "" && s.Provider == "" && s.Recipe.Provider == "" && s.ScreenHash == legacy)
}

func (r *Runner) Screen(ctx context.Context, o Options, docs []Document, families []Family) (Summary, error) {
	return r.screen(ctx, o, docs, families, nil)
}

func (r *Runner) screen(ctx context.Context, o Options, docs []Document, families []Family, prepared *screenBatch) (Summary, error) {
	var summary Summary
	if err := o.validate(); err != nil {
		return summary, err
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Wait == nil {
		r.Wait = wait
	}
	unlock, err := r.Repository.Lock(ctx)
	if err != nil {
		return summary, err
	}
	release := func() {
		if unlock != nil {
			unlock()
			unlock = nil
		}
	}
	defer release()
	rows, err := r.Repository.List(ctx)
	if err != nil {
		return summary, err
	}
	retained, err := r.verifyStore(rows, func(store retainedStore) error {
		if store.missingRows != 0 {
			return fmt.Errorf("submission ledger omits retained intents; run corpus submissions --rebuild before admission")
		}
		if !o.DryRun {
			return r.recoverLedgerSettlements(ctx, rows, store)
		}
		return nil
	})
	if err != nil {
		return summary, err
	}
	hash, legacyHash := screenHash(o), legacyScreenHash(o)
	for _, s := range rows {
		if matchesScreen(s, hash, legacyHash) && (s.State == "completed" || s.State == "cancelled") {
			summary = s.summary()
			if o.DryRun {
				return summary, nil
			}
			release()
			return summary, writeFamilies(o.Out, s)
		}
	}
	for i := range rows {
		s := &rows[i]
		if s.State == "preparing" || s.State == "uncertain" {
			known := retained.attachReceipt(s)
			if !known && retained.abandoned[s.ID] {
				s.State = "abandoned"
				known = true
			}
			if known && !o.DryRun {
				if err = r.Repository.Save(ctx, *s); err != nil {
					return summary, err
				}
			}
		}
		if s.State == "preparing" && r.Now().Sub(s.CreatedAt) > 10*time.Minute {
			s.State = "uncertain"
			if !o.DryRun {
				if err = r.Repository.Save(ctx, *s); err != nil {
					return summary, err
				}
			}
		}
		if s.State == "uncertain" {
			return summary, fmt.Errorf("uncertain classification submission %s blocks new work; reconcile it with corpus submissions", s.ID)
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		s := rows[i]
		if matchesScreen(s, hash, legacyHash) && s.State != "abandoned" {
			summary = s.summary()
			if o.DryRun {
				return summary, nil
			}
			if s.State == "completed" || s.State == "cancelled" {
				release()
				return summary, writeFamilies(o.Out, s)
			}
			if s.State != "submitted" && s.State != "cancelling" {
				return summary, fmt.Errorf("submission %s is %s; operator review required", s.ID, s.State)
			}
			release()
			return summary, r.poll(ctx, &s, o.Out)
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		s := rows[i]
		if s.RunID == o.RunID && nonterminal(s.State) {
			return summary, fmt.Errorf("earlier classification submission %s blocks this release", s.ID)
		}
	}
	entries, readErr := os.ReadDir(o.Out)
	if readErr != nil && !os.IsNotExist(readErr) {
		return summary, fmt.Errorf("inspect screening output: %w", readErr)
	}
	if len(entries) > 0 {
		return summary, fmt.Errorf("screening output directory is not empty")
	}
	if prepared == nil {
		release()
		ready, prepareErr := r.prepareScreenBatch(ctx, o, docs, families, indexingHistory(rows, retained))
		if prepareErr != nil {
			return summary, prepareErr
		}
		return r.screen(ctx, o, docs, families, ready)
	}
	s, requests, summary, creator, err := r.admitBatch(ctx, prepared, o)
	dispatched, finished := false, false
	finishCreator := func() error {
		if creator == nil || finished {
			return nil
		}
		finished = true
		if unlock == nil {
			releaseFinish, lockErr := r.Repository.Lock(context.WithoutCancel(ctx))
			if lockErr != nil {
				_ = creator.close()
				return lockErr
			}
			defer releaseFinish()
		}
		return r.finishCreator(s, creator, dispatched)
	}
	defer func() { _ = finishCreator() }()
	if err != nil || o.DryRun {
		return summary, err
	}
	if len(requests) == 0 {
		s.Settled = true
		return summary, writeFamilies(o.Out, s)
	}
	j := 0
	for i, item := range s.Items {
		if !item.Local {
			requests[j].CustomID = customID(s.ID, i)
			j++
		}
	}
	release()
	_, err = r.transition(ctx, s.ID, func(latest *Submission) (bool, error) {
		if latest.State != "preparing" || latest.BatchID != "" || latest.ItemsHash != s.ItemsHash {
			return false, fmt.Errorf("creator is no longer authorized to submit this attempt")
		}
		return false, r.writeCreatorTrace(*latest, "creator.dispatch", true)
	})
	if err != nil {
		return summary, err
	}
	dispatched = true
	b, err := r.providerCreate(ctx, s.ID, requests)
	if b.ID != "" {
		if !b.CreatedAt.IsZero() {
			s.SubmittedAt = &b.CreatedAt
		}
		if writeErr := r.withStoreLock(true, func() error { return r.recordBatch(&s, b.ID) }); writeErr != nil {
			return summary, writeErr
		}
	}
	if err != nil || b.ID == "" {
		_, saveErr := r.transition(ctx, s.ID, func(latest *Submission) (bool, error) {
			if latest.State != "preparing" {
				return false, nil
			}
			latest.State = "uncertain"
			return true, nil
		})
		if saveErr != nil {
			return summary, saveErr
		}
		if err != nil {
			return summary, fmt.Errorf("classification create response is uncertain; reconcile submission %s: %w", s.ID, providerError(err))
		}
		return summary, fmt.Errorf("classification create response is uncertain; reconcile submission %s", s.ID)
	}
	attached, err := r.transition(ctx, s.ID, func(latest *Submission) (bool, error) {
		if latest.BatchID != "" {
			if latest.BatchID != s.BatchID {
				return false, fmt.Errorf("submission has a different provider batch identity")
			}
			return false, nil
		}
		if latest.State != "preparing" && latest.State != "uncertain" {
			return false, fmt.Errorf("submission changed before provider batch attachment")
		}
		latest.BatchID = s.BatchID
		latest.SubmittedAt = s.SubmittedAt
		latest.DeadlineAt = s.DeadlineAt
		latest.State = "submitted"
		return true, nil
	})
	if err != nil {
		return summary, err
	}
	if err = finishCreator(); err != nil {
		return summary, err
	}
	return summary, r.poll(ctx, &attached, o.Out)
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
