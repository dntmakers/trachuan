# ADR 0003: PostgreSQL with one database per corpus release

Status: accepted 2026-11-08.

## Context

The corpus is 142k chunks with dense and sparse vectors, a structure tree,
relations and validity dates. banhmi measured 93.8% recall on PostgreSQL with
pgvector HNSW, sparsevec BM25 and reciprocal rank fusion.

## Decision

PostgreSQL with pgvector in a container on the host. Each corpus release is its
own database (`corpus_<cc>_<rel>`) activated by a pointer; accounts and tokens
live in a separate auth database that is never reloaded. The server never writes
corpus data; the pipeline never connects to the host. The host creates and
prepares empty release databases as `postgres` before handing ownership to
`activator` for law or `frameworks_activator` for frameworks. Each restore role
has `NOCREATEDB` and owns only its kind. In `auth`, `auth_owner` holds the
activation advisory lock and owns the schema, migrations and release writes. The
serving process connects as `auth_runtime` with explicit grants and no ownership
([database roles](../design/database-roles.md)).

## Consequences

Releases are atomic and reversible. Vector dimensions and model belong to the
release recipe. Two databases to back up; pgBackRest handles both.

## Alternatives rejected

SQLite with sqlite-vec (no ANN index, re-measure everything). A separate search
engine such as Meilisearch or Qdrant (second service, duplicated data). Managed
RDS (cost, less control).

## History

- 2026-11-08: accepted in the brainstorming conversation.
- 2026-11-08: added the `activator` role after design review finding S-01, which
  showed activation could not both write the pointer and obey "the pipeline
  never touches auth".
- 2026-11-08: widened the activator to the journal and retirement tables after
  finding S-25, so the recovery procedure can run under its own role.
- 2026-11-18: took the activation tables away from `activator`. `auth_owner`,
  the role the internet-facing server uses, owns them, so a compromised server
  could plant a trigger or view that ran as `activator`, which owns every corpus
  database. The updater now uses `auth_owner` for them and keeps each
  operation's record on the host, so a table row cannot drive it.
- 2026-11-28: separated law and frameworks restore ownership, moved the
  activation lock to `auth_owner`, and introduced `auth_runtime` to protect
  release pointers and entitlement history. The host verifies the rollback floor
  in a completed release before the maintenance cutover. Rollout follows
  [database roles](../design/database-roles.md); until it lands, `activator`
  still restores both kinds.
