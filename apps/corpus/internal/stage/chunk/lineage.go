package chunk

import (
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func checkExtractLineageForSnapshot(dir, fetchHash, inputHash string, snapshot stage.Snapshot, recipe string) error {
	contract, err := stage.ExtractContract(snapshot, recipe)
	if err != nil {
		return err
	}
	return fetch.CheckExtractLineageContract(dir, fetchHash, inputHash, contract)
}
