//go:build !darwin

package pdftext

import "context"

func readPlatformIdentity(_ []string) (platformIdentity, error) {
	return platformIdentity{}, ErrConfiguration
}

func readPlatformIdentityContext(ctx context.Context, _ []string) (platformIdentity, error) {
	if err := ctx.Err(); err != nil {
		return platformIdentity{}, err
	}
	return platformIdentity{}, ErrConfiguration
}
