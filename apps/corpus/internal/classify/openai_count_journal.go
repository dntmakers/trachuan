package classify

import (
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

type openAICountPair struct {
	dispatch openAICountDispatch
	result   *openAICountResult
}
type openAICountEstimateValue struct {
	MicroUSD int64
	Unknown  int64
}

func (r *Runner) openAICountEstimate() (openAICountEstimateValue, error) {
	return r.openAICountEstimateWithSync(unix.Fsync)
}
func (r *Runner) openAICountEstimateWithSync(syncDir func(int) error) (openAICountEstimateValue, error) {
	var estimate openAICountEstimateValue
	err := r.withOpenAICountStore(false, syncDir, func(s *openAICountStore) error {
		var err error
		if err = s.barrier(); err != nil {
			return err
		}
		pairs, err := s.journalSnapshot()
		if err != nil {
			return err
		}
		for _, p := range pairs {
			if p.result == nil || p.result.Unknown {
				if estimate.Unknown == math.MaxInt64 {
					return fmt.Errorf("OpenAI estimate overflow")
				}
				estimate.Unknown++
				continue
			}
			price, _ := openAIPriceForModel(p.dispatch.Model)
			rate := price.input * 2
			if p.result.InputTokens > 272000 {
				rate *= 2
			}
			cost, e := openAICost([][2]int64{{rate, p.result.InputTokens}})
			if e != nil || cost > math.MaxInt64-estimate.MicroUSD {
				return fmt.Errorf("OpenAI estimate overflow")
			}
			estimate.MicroUSD += cost
		}
		return nil
	})
	if errors.Is(err, errStoreRootMissing) {
		return estimate, nil
	}
	if err != nil {
		return openAICountEstimateValue{}, err
	}
	return estimate, nil
}
