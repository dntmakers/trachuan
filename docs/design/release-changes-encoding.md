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

The payload is JSON text that the shared projection code writes once, in this
version 1 form: one object, keys in ascending byte order at every level, no
white space outside strings, strings in UTF-8 where `"` is written `\"`, `\` is
written `\\`, a code point below U+0020 is written `\u00` and two lower-case hex
digits, and every other code point is written as itself, never as an escape;
integers in decimal with no leading zero, zero as `0`, a negative with one
leading `-`; and `true`, `false` and `null` literals. No other number form
occurs. The release stores that text unchanged in a column
`payload text NOT NULL CHECK (payload IS JSON OBJECT WITH UNIQUE KEYS)`, not
`jsonb`, and every later reader hashes the stored bytes as they are and never
encodes the JSON again. A field holding a TAB, LF or NUL, a kind outside the
served list, an ordinal that is not the next integer from 1 or a baseline with
rows is an error for writers and readers alike. A fixture in the repository
holds a projection's bytes and its expected digest, and each implementation
tests against it; it covers a quote, a backslash, a control character, non-ASCII
text, zero, a negative integer, null and an absent field.
