package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"danny.vn/trachuan/internal/frameworks/serve"
	"github.com/stretchr/testify/require"
)

func TestResolver_FrameworksOpenLogsOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status string
	}{
		{name: "opened", status: "opened"},
		{name: "failed", err: errors.New("private error"), status: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			r := &Resolver{
				Auth: &fakeStore{active: "frameworks_20990103"}, Clock: time.Now,
				Frameworks: &serve.Options{}, Logger: slog.New(slog.NewJSONHandler(&output, nil)),
				OpenFrameworks: func(context.Context, string) (*serve.Engine, func(), error) {
					if tc.err != nil {
						return nil, nil, tc.err
					}
					return &serve.Engine{}, nil, nil
				},
			}
			t.Cleanup(r.Close)
			result := r.refreshFrameworks(t.Context())
			require.NotNil(t, result)
			open := <-result
			if tc.err != nil {
				require.ErrorIs(t, open.Err, tc.err)
			} else {
				require.NoError(t, open.Err)
			}
			var record map[string]any
			decoder := json.NewDecoder(&output)
			require.NoError(t, decoder.Decode(&record))
			require.Contains(t, record, "time")
			delete(record, "time")
			require.Equal(t, map[string]any{"level": "INFO", "msg": "frameworks release", "release": "20990103", "status": tc.status}, record)
			require.ErrorIs(t, decoder.Decode(&record), io.EOF)
		})
	}
}
