# ADR 0002: Evidence only; Vietnamese queries; no platform model

Status: accepted 2026-11-08.

## Context

Sending user questions to a model provider abroad is a cross-border transfer
under Vietnam's personal data law (91/2025/QH15, Điều 20). Machine translation
of Vietnamese legal terms is unreliable.

## Decision

Our servers run only the query embedder. The user's own agent reasons over
verbatim evidence. Search accepts Vietnamese with or without diacritics and
recognizes document numbers. Retrieval may restore diacritics from the release's
own text under the [retrieval recipe](../design/retrieval-recipe.md). Legal text
is never translated. Questions are never stored.

## Consequences

No answer quality to defend; no question data to protect. Users without an AI
client get search and reading, not answers.

## Alternatives rejected

A platform-run answer model or reranker. Translating questions. Query history
features.

## History

- 2026-11-08: accepted in the brainstorming conversation.
- 2026-11-11: the owner allowed in-memory server caches to cut latency. Cache
  keys are digests of the query and request, values are vectors or responses,
  and nothing is written to disk, logs or backups; questions are still never
  stored ([caching](../design/caching.md)).
- 2026-11-11: agent error reports carry structured fields only (handle, kind,
  closed values, dates, document numbers checked against the corpus), never free
  text or URLs; only a capped covert channel through the choice of values
  remains ([error reports](../design/error-reports.md)).
- 2026-11-25 The owner approved queries typed without diacritics. Retrieval
  recipe 4 restores them from release text before embedding. The study and
  trade-offs are in
  [search without diacritics](../design/unaccented-queries.md).
