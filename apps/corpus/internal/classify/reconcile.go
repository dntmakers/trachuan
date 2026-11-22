package classify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

type batchLister interface {
	StreamPages(context.Context, func([]anthropic.MessageBatch, string) error) error
}

func (r *Runner) Reconcile(ctx context.Context, id string, abandon bool) error {
	if !validID(id) {
		return fmt.Errorf("invalid submission id")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	target, err := r.transition(ctx, id, func(s *Submission) (bool, error) {
		if s.BatchID == "" && s.State != "uncertain" && s.State != "abandoned" && (s.State != "failed" || abandon) {
			return false, fmt.Errorf("cannot reconcile submission in state %s without a provider batch identity", s.State)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if abandon {
		if err = r.checkAbandonmentReceipt(target); err != nil {
			return err
		}
	}
	if abandon && target.State == "abandoned" {
		return nil
	}
	if !abandon {
		if target.Settled {
			return nil
		}
		if target.BatchID == "" {
			receipt := target
			if found, restoreErr := r.restoreReceipt(&receipt); restoreErr != nil {
				return restoreErr
			} else if found {
				return r.reconcileKnownBatch(ctx, target, receipt.BatchID)
			}
		} else {
			return r.reconcileKnownBatch(ctx, target, target.BatchID)
		}
	}
	lister, ok := r.Client.(batchLister)
	if !ok {
		return fmt.Errorf("provider batch listing is unavailable")
	}
	before := r.Now().Add(time.Minute)
	if minimum := target.CreatedAt.Add(10 * time.Minute); before.Before(minimum) {
		before = minimum
	}
	batches, err := r.captureListing(ctx, lister, target.ID, target.CreatedAt.Add(-time.Minute), before)
	if err != nil {
		return err
	}
	allEnded := true
	for _, batch := range batches {
		if batch.ProcessingStatus != "ended" {
			allEnded = false
			continue
		}
		if resultErr := r.captureResults(ctx, target.ID, batch.ID, batch.CreatedAt); resultErr != nil {
			return resultErr
		}
		if recordErr := r.recordKnownDownload(ctx, batch.ID); recordErr != nil {
			return recordErr
		}
	}
	if _, found, recordErr := r.recordDownloaded(ctx, target, ""); found || recordErr != nil {
		return recordErr
	}
	if !abandon {
		return fmt.Errorf("no ended provider batch matches submission")
	}
	if !allEnded || r.Now().Before(target.CreatedAt.Add(creatorQuietWindow)) {
		return fmt.Errorf("cannot abandon before the 24-hour provider window plus ten-minute margin or while candidate batches remain active")
	}
	creator, lockErr := r.lockCreator(id)
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = creator.close() }()
	_, err = r.transition(ctx, id, func(latest *Submission) (bool, error) {
		if !sameReconciliationTarget(target, *latest) {
			return false, fmt.Errorf("submission changed during reconciliation; retry from its current state")
		}
		if receiptErr := r.checkAbandonmentReceipt(*latest); receiptErr != nil {
			return false, receiptErr
		}
		quietSince, quietErr := r.creatorQuietSince(target)
		if quietErr != nil {
			return false, quietErr
		}
		if r.Now().Before(quietSince.Add(creatorQuietWindow)) {
			return false, fmt.Errorf("cannot abandon before the creator has been quiet for 24 hours plus ten minutes")
		}
		if writeErr := r.writeAbandonment(latest); writeErr != nil {
			return false, writeErr
		}
		latest.State = "abandoned"
		return true, nil
	})
	return err
}

func (r *Runner) reconcileKnownBatch(ctx context.Context, target Submission, batchID string) error {
	current, _, recoverErr := r.recordDownloaded(ctx, target, batchID)
	if current.Settled || (recoverErr != nil && !errors.Is(recoverErr, errUnpricedAttempts)) {
		return recoverErr
	}
	if r.Client == nil {
		return fmt.Errorf("reconciliation requires a provider client")
	}
	batch, err := r.Client.Get(ctx, batchID)
	if err != nil {
		return providerError(err)
	}
	if batch.ProcessingStatus != "ended" {
		return fmt.Errorf("classification provider batch has not ended")
	}
	if err = r.captureResults(ctx, target.ID, batchID, batch.CreatedAt); err != nil {
		return err
	}
	_, _, err = r.recordDownloaded(ctx, target, batchID)
	return err
}

func sameReconciliationTarget(a, b Submission) bool {
	return a.KeyVersion == b.KeyVersion && a.PriceVersion == b.PriceVersion && a.State == b.State && a.BatchID == b.BatchID && a.ItemsHash == b.ItemsHash && a.ScreenHash == b.ScreenHash && a.Recipe.Hash() == b.Recipe.Hash() && a.CreatedAt.Equal(b.CreatedAt) && a.ReserveMicroUSD == b.ReserveMicroUSD && a.Settled == b.Settled && a.CostMicroUSD == b.CostMicroUSD
}

func (r *Runner) checkAbandonmentReceipt(s Submission) error {
	if s.BatchID != "" {
		return fmt.Errorf("cannot abandon a submission with a provider batch identity")
	}
	found, err := r.restoreReceipt(&s)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("cannot abandon a submission with a retained provider receipt")
	}
	return nil
}
