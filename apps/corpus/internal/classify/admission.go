package classify

import (
	"context"
	"fmt"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func (r *Runner) admitBatch(ctx context.Context, prepared *screenBatch, o Options) (Submission, []batchapi.Request, Summary, *creatorGuard, error) {
	var s Submission
	var requests []batchapi.Request
	var summary Summary
	var creator *creatorGuard
	err := r.Repository.Admission(ctx, func() error {
		rows, err := r.Repository.List(ctx)
		if err != nil {
			return err
		}
		_, err = r.verifyStore(rows, func(retained retainedStore) error {
			if retained.missingRows != 0 {
				return fmt.Errorf("submission ledger omits retained intents; run corpus submissions --rebuild before admission")
			}
			total, ledgerErr := retained.ledgerCost()
			if ledgerErr != nil {
				return ledgerErr
			}
			s, requests, summary, err = r.selectBatch(prepared, rows, retained)
			if err != nil {
				return err
			}
			summary.LedgerMicroUSD = total
			summary.Admitted = len(requests) == 0 || (total <= s.BudgetMicroUSD && s.ReserveMicroUSD <= s.BudgetMicroUSD-total)
			if o.DryRun {
				return nil
			}
			if !summary.Admitted {
				return fmt.Errorf("classification batch refused by %s budget", o.Budget)
			}
			if len(requests) == 0 {
				return nil
			}
			if r.Client == nil {
				return fmt.Errorf("classification submission requires a provider client")
			}
			identity, ok := r.Client.(interface{ AccountFingerprint() string })
			if !ok || !validID(identity.AccountFingerprint()) {
				return fmt.Errorf("classification provider account identity is unavailable")
			}
			s.AccountFingerprint = identity.AccountFingerprint()
			if s.ID, err = newID(); err != nil {
				return err
			}
			summary.SubmissionID = s.ID
			s.ItemsHash = itemsHash(s.Items)
			s.CreatedAt = r.Now().UTC().Truncate(time.Microsecond)
			if err = r.Repository.Save(ctx, s); err != nil {
				return err
			}
			if err = r.writeIntent(s); err != nil {
				return err
			}
			creator, err = r.lockCreator(s.ID)
			return err
		})
		return err
	})
	return s, requests, summary, creator, err
}
