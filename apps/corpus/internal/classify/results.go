package classify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

var (
	providerToken     = regexp.MustCompile(`^[a-z_]{1,64}$`)
	providerRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

func providerField(value string, pattern *regexp.Regexp, missing string) string {
	if pattern.MatchString(value) {
		return value
	}
	return missing
}

func providerError(err error) error {
	var writeErr *captureWriteError
	if errors.As(err, &writeErr) {
		return writeErr
	}
	var api *anthropic.Error
	if errors.As(err, &api) {
		return fmt.Errorf("classification provider request failed: status=%d type=%s code=%s request_id=%s", api.StatusCode,
			providerField(string(api.Type()), providerToken, "unknown"), providerField(string(api.ErrorCode()), providerToken, "none"),
			providerField(api.RequestID, providerRequestID, "unknown"))
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("classification provider request interrupted: %w", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("classification provider request timed out: %w", context.DeadlineExceeded)
	}
	return fmt.Errorf("classification provider request failed")
}

func (r *Runner) poll(ctx context.Context, s *Submission, out string) error {
	if s.DeadlineAt == nil {
		return fmt.Errorf("submission has no polling deadline")
	}
	for {
		current, err := r.transition(ctx, s.ID, func(latest *Submission) (bool, error) {
			if settled(*latest) {
				return false, nil
			}
			rows, listErr := r.Repository.List(ctx)
			if listErr != nil {
				return false, listErr
			}
			var found bool
			_, restoreErr := r.verifyStore(rows, func(retained retainedStore) error {
				var err error
				found, err = r.recoverRetained(latest, retained)
				return err
			})
			if latest.Settled || restoreErr != nil {
				return found, restoreErr
			}
			if stateErr := pollingState(latest); stateErr != nil {
				return false, stateErr
			}
			if !r.Now().Before(*latest.DeadlineAt) {
				latest.State = "failed"
				return true, nil
			}
			return found, nil
		})
		if err != nil {
			return err
		}
		if settled(current) {
			return writeFamilies(out, current)
		}
		if err = pollingState(&current); err != nil {
			return err
		}
		if r.Client == nil {
			return fmt.Errorf("classification polling requires a provider client")
		}
		b, err := r.Client.Get(ctx, current.BatchID)
		if err != nil {
			return providerError(err)
		}
		if b.ProcessingStatus == "ended" {
			if resultErr := r.captureResults(ctx, current.ID, current.BatchID, b.CreatedAt); resultErr != nil {
				return resultErr
			}
			finished, _, finishErr := r.recordDownloaded(ctx, current, current.BatchID)
			if finishErr != nil {
				return finishErr
			}
			return writeFamilies(out, finished)
		}
		delay := min(5*time.Minute, current.DeadlineAt.Sub(r.Now()))
		if delay <= 0 {
			continue
		}
		if err = r.Wait(ctx, delay); err != nil {
			return fmt.Errorf("wait for classification batch: %w", err)
		}
	}
}
