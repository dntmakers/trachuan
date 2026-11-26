package evidence

import (
	"context"
	"io"

	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
)

const identityFile = "identity.jsonl"
const changesFile = "changes.jsonl"
const summarySchema = 1
const lineLimit = 1 << 20

type inputError = releasechange.InputError
type members = releasechange.Members
type summarySource = releasechange.SummarySource
type identityBindings = releasechange.IdentityBindings
type changesBindings = releasechange.ChangesBindings
type identityHeader = releasechange.IdentityHeader
type changesHeader = releasechange.ChangesHeader

func writeIdentity(ctx context.Context, w io.Writer, src summarySource, m members, b identityBindings) error {
	return releasechange.WriteIdentity(ctx, w, src, m, b)
}

func strictDecode(data []byte, out any) error { return releasechange.StrictDecode(data, out) }
