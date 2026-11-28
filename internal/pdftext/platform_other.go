//go:build !darwin

package pdftext

func readPlatformIdentity(_ []string) (platformIdentity, error) {
	return platformIdentity{}, ErrConfiguration
}
