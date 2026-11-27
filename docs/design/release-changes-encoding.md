# Release change projection encoding

Part of [serving release changes](release-changes-serving.md). This note owns
the bytes behind the projection digest, so the build, the evidence stage,
signing and the host release check all hash the same thing.

## Lines

The projection digest is SHA-256 over bytes that every reader can rebuild
without a JSON library: a header line, then one line per row in ordinal order,
each ending in LF, fields joined by one TAB, all UTF-8. The header line is
`v<projection version>`, country, release, predecessor release, predecessor
identity hash as 64 lower-case hex digits with no prefix, and `baseline` as `0`
or `1`. A row line is the ordinal in decimal with no sign or leading zero, the
kind, the two filter keys and the payload. An absent predecessor, hash or filter
key is the empty field and is stored as SQL `NULL`; a stored empty string is an
error, so absent has one form.

The payload is JSON text that the shared projection code writes once, in the
form of projection version 2: one object, keys in ascending byte order at every
level, no white space outside strings, strings in UTF-8 where `"` is written
`\"`, `\` is written `\\`, a code point below U+0020 is written `\u00` and two
lower-case hex digits, and every other code point is written as itself, never as
an escape; integers in decimal with no leading zero, zero as `0`, a negative
with one leading `-`; and `true`, `false` and `null` literals. No other number
form occurs. The release stores that text unchanged in a column
`payload text NOT NULL CHECK (payload IS JSON OBJECT WITH UNIQUE KEYS)`, not
`jsonb`, and every later reader hashes the stored bytes as they are and never
encodes the JSON again. A field holding a TAB, LF or NUL, a kind outside the
served list, an ordinal that is not the next integer from 1 or a baseline with
rows is an error for writers and readers alike. A fixture in the repository
holds a projection's bytes and its expected digest, and each implementation
tests against it; it covers a quote, a backslash, a control character, non-ASCII
text, zero, a negative integer, null and an absent field.

Upload and signing in `deploy/github` compute the projection from
`changes.jsonl` with the Python standard library, so the signing job needs no Go
toolchain or module download. That code is a second implementation of the
projection rules, not a copy of the Go output: it reads the same sidecar,
applies the same schema, kind and size checks, and must reproduce both the
encoding fixture and the projection of the change-record fixture byte for byte.
CI runs both implementations over the same fixtures, so a rule changed in one
and not the other fails the build.

The host updater keeps its own copy of the projection version and kind set for
the `release-check` report. A committed fixture holds a `release-check` report;
a Go test keeps it equal to the Go output and an updater test reads it, so a
difference fails CI rather than the first production activation.

## Version 2

Version 2 is the first published projection. Version 1 also served
`provision_validity`, which no build path writes, and was never in a published
corpus release; every writer and reader accepts exactly version 2 and refuses
version 1, including a version 1 set row, `build.json` and signed manifest.
Corpus schema 9 changes in place with it: `release_change_set` drops the
`provision_validity` count column, both checks drop the kind, and `build.json`
`changes_counts` drops the key. The schema file is part of the build recipe, so
a build made before the change is refused at planning and the release needs a
new run. The signed sidecar keeps the `provision_validity` record type. Serving
provision validity later needs a build path that writes it and projection
version 3.

Images that read only version 1 refuse a version 2 manifest in the updater's
`changes_projection` check before the release sequence is bound, and in
`release-check`. They still open a schema 9 database, so after a version 2
release is active only the release floor keeps an image rollback from serving
search with a failing `changes`. Ship the first image whose updater and server
accept version 2, raise the floor to it in the next image, then upload and sign.

Proof for version 2: a sidecar `provision_validity` record with `provenance` and
`ambiguous` both false yields no row; `kinds: ["provision_validity"]` is
`invalid_request`; `counts` has no such key; version 1 inputs are refused.

`changes` drops the kind from `kinds`, `counts` and the item schemas. This is
the one exception to additive tool schemas in [retrieval](retrieval.md): every
production release when the kind was dropped predated schema 9, so the tool has
only returned `state: "unavailable"` and no caller has received counts, rows or
a cursor.
