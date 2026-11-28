//go:build !darwin

package pdftext

import "context"

func readPlatformIdentityContext(ctx context.Context, _ []string) (platformIdentity, error) {
	if err := ctx.Err(); err != nil {
		return platformIdentity{}, err
	}
	return platformIdentity{}, ErrConfiguration
}
