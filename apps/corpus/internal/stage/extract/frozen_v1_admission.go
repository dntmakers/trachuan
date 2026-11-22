package extract

import (
	"fmt"
	"reflect"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func admitExistingV1(env stage.Env, art artifact.Result, fetchHash, inputHash, source string, expected detectorInputsV1) error {
	data, err := artifact.ReadVerifiedFile(env.Artifacts.Abs(art.Path), "text.json", art.SHA256, 256<<20)
	if err != nil {
		return fmt.Errorf("verify cached extract text: %w", err)
	}
	actualSource, actual, err := fetch.VerifyExtractLineageV1Bytes(data, fetchHash, inputHash)
	if err != nil {
		return fmt.Errorf("verify cached extract lineage: %w", err)
	}
	if actualSource != source || !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("cached extract detector inputs differ from frozen derivation")
	}
	return nil
}
