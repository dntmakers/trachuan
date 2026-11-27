package build

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func projectionReadError(err error) error {
	var invalid *corpusdb.InvalidProjectionError
	if errors.As(err, &invalid) {
		return fmt.Errorf("%w: %w", errInvalidDatabase, err)
	}
	return err
}

func validateProjectionRecord(record BuildRecord, run stage.Run) error {
	digest, err := hex.DecodeString(record.ChangesProjection.SHA256)
	if record.ChangesProjection.Version != releasechanges.Version || err != nil || len(digest) != 32 || record.ChangesProjection.SHA256 != strings.ToLower(record.ChangesProjection.SHA256) {
		return fmt.Errorf("invalid build projection digest or version")
	}
	header, err := ProjectionHeader(run)
	if err != nil {
		return fmt.Errorf("invalid build projection header: %w", err)
	}
	hasher, err := releasechanges.NewHasher(header)
	if err != nil {
		return fmt.Errorf("invalid build projection header: %w", err)
	}
	kinds := hasher.Counts()
	if len(record.ChangesCounts) != len(kinds) {
		return fmt.Errorf("invalid build projection counts")
	}
	for kind := range kinds {
		count, ok := record.ChangesCounts[kind]
		if !ok || count < 0 || (header.Baseline && count != 0) {
			return fmt.Errorf("invalid build projection counts")
		}
	}
	return nil
}

func ProjectionMembers(inv Inventory, recordedHash string) (releasechange.Members, error) {
	if inv.Version != 2 {
		return releasechange.Members{}, &releasechange.InputError{Reason: "identity summary needs the run's frozen membership inventory"}
	}
	return releasechange.NewMembers(inv.Membership.Entries, inv.IdentityHash(), recordedHash)
}

func CompareProjection(record BuildRecord, expected releasechanges.Header, actual corpusdb.ProjectionResult) error {
	if !reflect.DeepEqual(expected, actual.Header) || record.ChangesProjection.Version != actual.Header.Version || record.ChangesProjection.SHA256 != actual.Digest || !maps.Equal(record.ChangesCounts, actual.Counts) {
		return fmt.Errorf("release change projection mismatch")
	}
	return nil
}
