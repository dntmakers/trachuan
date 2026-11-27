package run

import (
	"encoding/json"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
)

type Deps struct {
	Artifacts                                artifact.Store
	TaskQueue                                string
	BatchesInFlight                          int
	FetchBatchesInFlight                     int
	HeartbeatTimeout                         time.Duration
	DB                                       *pgxpool.Pool
	Temporal                                 client.Client
	RecipesFor                               func(stage.Snapshot) map[string]string
	Stages                                   []string
	SourceList                               json.RawMessage
	PreviousReport                           ReportChoice
	ClassificationSetSHA256, TaxonomyVersion string
	NoClassificationSet                      bool
	Notice                                   func(string)
	Now                                      func() time.Time
}
