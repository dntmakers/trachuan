package publicsample

import (
	"context"
	"errors"
)

func attemptReason(ctx context.Context, body []byte, err error) Reason {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return ReasonCanceled
	}
	var reason Reason
	if errors.As(err, &reason) {
		switch reason {
		case ReasonAbstain, ReasonNoHit, ReasonWrongDocument, ReasonWrongNumber, ReasonNoProvenCut, ReasonMismatch, ReasonSchema, ReasonBodyCap:
			return reason
		}
	}
	if err != nil || len(body) == 0 {
		return ReasonSearchError
	}
	return ReasonSuccess
}
