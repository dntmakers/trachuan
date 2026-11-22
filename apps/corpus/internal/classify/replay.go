package classify

import (
	"context"
	"fmt"
	"sort"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type attemptRef struct{ row, item int }

type keyHistory struct {
	rows   []Submission
	keys   map[string][]attemptRef
	counts map[string]int64
}

func indexingHistory(rows []Submission, retained retainedStore) keyHistory {
	states := make(map[string]string, len(rows))
	for _, row := range rows {
		states[row.ID] = row.State
	}
	rows = append([]Submission(nil), retained.intents...)
	for i := range rows {
		s := &rows[i]
		if state, found := states[s.ID]; found {
			s.State = state
		}
		retained.attachReceipt(s)
		if settled, found := retained.settlements[s.ID]; found {
			s.Settled, s.State = true, settled.State
		} else if retained.abandoned[s.ID] {
			s.State = "abandoned"
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	history := keyHistory{rows: rows, keys: map[string][]attemptRef{}, counts: map[string]int64{}}
	for i, s := range rows {
		if s.KeyVersion != keyVersion {
			continue
		}
		for j, item := range s.Items {
			if !item.Local {
				if history.counts[item.RequestHash] == 0 {
					history.counts[item.RequestHash] = item.InputTokens
				}
				history.keys[item.RequestHash] = append(history.keys[item.RequestHash], attemptRef{i, j})
			}
			if item.ArticleMeta != nil {
				for key, n := range item.ArticleMeta.CountedRequests {
					if history.counts[key] == 0 {
						history.counts[key] = n
					}
				}
			}
		}
	}
	return history
}

func (r *Runner) countWith(h keyHistory, counted map[string]int64) TokenCounter {
	return func(ctx context.Context, p anthropic.MessageNewParams) (int64, error) {
		key, err := requestKey(&batchapi.Request{Params: p})
		if err != nil {
			return 0, err
		}
		n, err := readCount(r.countPath(key), key)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			n = h.counts[key]
		}
		if n == 0 {
			n, err = r.countRequestTokens(ctx, p)
			if err != nil {
				return 0, err
			}
			if err = r.writeCount(key, n); err != nil {
				return 0, &captureWriteError{err}
			}
		}
		if counted != nil {
			counted[key] = n
		}
		return n, nil
	}
}

type keyDisposition string

const (
	keySubmit    keyDisposition = ""
	keyReplay    keyDisposition = "replay"
	keyExhausted keyDisposition = "exhausted"
	keyHeld      keyDisposition = "held"
	keyUnknown   keyDisposition = "unknown"
)

func (r *Runner) replay(s Submission, item Item, h keyHistory, retained retainedStore) (Outcome, keyDisposition, error) {
	var held keyDisposition
	var failures int
	for _, ref := range h.keys[item.RequestHash] {
		prior := h.rows[ref.row]
		if prior.State == "abandoned" && retained.abandoned[prior.ID] && retained.retryAbandoned[prior.ID] {
			continue
		}
		if prior.BatchID == "" {
			if prior.Settled {
				return Outcome{}, keySubmit, fmt.Errorf("settled classification submission has no provider batch identity")
			}
			held = r.holdKey(held, prior, false)
			continue
		}
		a, msg, found, err := r.readAttempt(prior, ref.item)
		if err != nil {
			return Outcome{}, keySubmit, err
		}
		priced := found && a.Cost != nil
		if prior.Settled && !priced {
			return Outcome{}, keySubmit, fmt.Errorf("missing priced evidence for settled classification key")
		}
		if found && a.Outcome == "final" {
			return resultOutcome(s, item, msg, true), keyReplay, nil
		}
		if !prior.Settled {
			held = r.holdKey(held, prior, priced)
		}
		if found {
			failures++
		}
	}
	if held != keySubmit {
		return noResult(s, item), held, nil
	}
	if failures >= 3 {
		return noResult(s, item), keyExhausted, nil
	}
	return Outcome{}, keySubmit, nil
}

func (r *Runner) holdKey(prior keyDisposition, s Submission, priced bool) keyDisposition {
	expired := s.DeadlineAt != nil && !r.Now().Before(*s.DeadlineAt)
	if prior == keyUnknown || (!priced && (!nonterminal(s.State) || expired)) {
		return keyUnknown
	}
	return keyHeld
}

func (r *Runner) selectBatch(prepared *screenBatch, rows []Submission, retained retainedStore) (Submission, []batchapi.Request, Summary, error) {
	s := prepared.submission
	s.Items = append([]Item(nil), s.Items...)
	var summary Summary
	var requests []batchapi.Request
	size := len(`{"requests":[]}`)
	s.Results = map[string]Outcome{}
	h := indexingHistory(rows, retained)
	keys := make(map[string]bool, len(prepared.requests))
	for i, req := range prepared.requests {
		item := &s.Items[i]
		if keys[item.RequestHash] {
			return s, nil, summary, fmt.Errorf("duplicate classification request key")
		}
		keys[item.RequestHash] = true
		outcome, disposition, err := r.replay(s, *item, h, retained)
		if err != nil {
			return s, nil, summary, err
		}
		if disposition != keySubmit {
			item.Local = true
			item.LocalReason = disposition
			summary.addGap(disposition)
			s.Results[item.Key] = outcome
			continue
		}
		reserve, err := s.Recipe.reserve(s.PriceVersion, item.InputTokens)
		if err != nil {
			return s, nil, summary, err
		}
		body, err := req.Encode()
		if err != nil {
			return s, nil, summary, err
		}
		if len(requests) > 0 {
			size++
		}
		size += len(body) + len(`{"custom_id":"","params":}`) + 39
		requests = append(requests, req)
		s.ReserveMicroUSD += reserve
		summary.InputTokens += item.InputTokens
	}
	if len(s.Items) > 100000 || size > 256000000 {
		return s, nil, summary, fmt.Errorf("classification batch exceeds provider limit")
	}
	summary.Requests = len(requests)
	summary.ReserveMicroUSD = s.ReserveMicroUSD
	return s, requests, summary, nil
}

func (r *Runner) countRequestTokens(ctx context.Context, p anthropic.MessageNewParams) (int64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("classification token count requires a provider client")
	}
	return r.Client.CountTokens(ctx, p)
}
