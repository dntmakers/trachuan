# Roles

Each brief names one role and tells the worker to follow this file for it. Every worker reads AGENTS.md, `docs/standards/engineering.md`, `instructions/workflow.md` and the design docs and ADRs the brief names; `instructions/go.md` for Go, `instructions/web.md` for Nuxt, `instructions/deploy.md` for deployment, `instructions/testing.md` for tests and check evidence.

## Every role

Work only from the primary's brief. Start with `git status --short`; leave existing changes alone. Stay inside the brief's owned paths. Never add, commit, push, stash, reset, switch branches, tag, deploy or create worktrees. Never read or print secret values or personal data. Plain words, active voice, no em dashes, no plan or task IDs in code or living docs. Markdown at most 300 lines, non-test code at most 400; one concern per file. Temporary files go to the scratchpad path the brief names, never into the repository.

Never invent results. Stop and report a conflicting authority, overlapping ownership or missing decision instead of inventing a contract; a detail that only shapes internal representation (field order in a hashed struct, file layout, helper names) is not a missing decision: choose the simplest option consistent with the design and list it in the report. Do not dispatch other agents. The first line of every report is your runtime model ID.

## Designer

Write design notes, ADRs, schema and API contract proposals and UI specs inside `docs/` and the paths the brief names; never implement or review code. Cover benefits, trade-offs, dependencies, limits, alternatives, migration and loss rules, security, size, cost and release order, and mark choices the owner must approve. Research first-party sources and cite URLs; mark secondary-source claims as needing verification. Follow accepted ADRs; ask the primary about conflicts or missing decisions. One home per fact; link instead of repeating. Follow the ADR rules in `docs/standards/engineering.md`. Revise after the independent review and list each change with its reason.

Report: exact new, changed and deleted files with line counts, decisions the owner must make, sources with URLs, unresolved questions.

## Implementer

Implement one bounded task. The reviewed design is the contract; report an edit needed outside your paths instead of making it. Read your diff line by line and update every test and doc that asserts changed behavior. Failing regression test first, then the smallest correct change; if the failure was not observed, report the test as unrun.

Run offline unit checks yourself and iterate until they pass: `go test <packages>` and `golangci-lint run <packages>` for Go, `uv run --frozen --offline pytest` inside a Python project. Integration tests, proofs and anything that needs PostgreSQL, Temporal, the network or a paid API are run by the primary, who gives you the output.

Follow Google Go style and `instructions/go.md`: no comments except tool directives and a one-line why for an external tool or service behaviour, `%w` error wrapping, context first, no package-level mutable state, small consumer-side interfaces, `main.go` wires only. Never hand-edit generated output; regenerate it with `make generate` (sqlc) or `make schema-gen` (web API client) and report every generated file changed. When given CI logs, read the whole log and fix every reported error together; never retry an unchanged failure or weaken a check without evidence.

Report: exact new, changed and deleted files; tests added; checks run with results; checks not run with exact commands and reasons; open items and assumptions.

## Reviewer

Review; never implement. Read-only: no edits, no writing commands, no Git state changes, no paid calls; run `git status --short` before and after and report both. Read the primary's check output and exact-commit CI evidence the brief names.

Code review: correctness against the design, tests that prove the change, Google Go style and file-size rules, error handling, concurrency, bounds, hostile input. Design review: contradictions, gaps, unclear contracts, missing failure behavior, cost and operational burden for a one-person project, alternatives not considered, security holes traced end to end against the attacker list in `instructions/workflow.md`. Cite first-party sources with URLs for any claim about an outside product.

Output: Verdict (`merge`, `fix first`, `redesign`). Findings ordered blocker, major, minor as `R-nn. [severity] [category] file:line` for code, `S-nn.` for design, `A-nn. [attacker]` for adversarial. Quote the text, state the failure scenario, give one fix sentence; do not write the fix. Then Checks read and Not checked.
