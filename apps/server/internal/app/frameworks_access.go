package app

import (
	"context"

	"danny.vn/trachuan/apps/server/internal/frameworkaccess"
	frameworkserve "danny.vn/trachuan/internal/frameworks/serve"
)

func frameworkEntitled(auth frameworkaccess.Store) func(context.Context, int64) bool {
	return func(ctx context.Context, accountID int64) bool {
		ctx, cancel := context.WithTimeout(ctx, authQueryDeadline)
		defer cancel()
		service := frameworkaccess.Service{Store: frameworkaccess.RequestStore(ctx, auth)}
		return service.Entitled(ctx, accountID)
	}
}

func frameworkCaller(auth frameworkaccess.Store) func(context.Context, int64, []string) (frameworkserve.Caller, error) {
	return func(ctx context.Context, id int64, codes []string) (frameworkserve.Caller, error) {
		ctx, cancel := context.WithTimeout(ctx, authQueryDeadline)
		defer cancel()
		access := frameworkaccess.Service{Store: frameworkaccess.RequestStore(ctx, auth), Frameworks: codes}
		set, err := access.AccessSet(ctx, id)
		if err != nil {
			set = frameworkaccess.EmptySet(codes)
		}
		return frameworkserve.Caller{Readable: func(code, class string) bool {
			return frameworkaccess.Decide(code, class, set) == frameworkaccess.Full
		}, AccessUnavailable: err != nil}, nil
	}
}
