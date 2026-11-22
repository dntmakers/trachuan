@AGENTS.md

## Claude Code specifics

- The primary session runs Claude Code's default prompt with this file and `CLAUDE.local.md`; the owner picks its model with `/model`. Duties are in AGENTS.md "Agents" and `instructions/workflow.md`.
- Agent dispatch, model routes and fallbacks are in `instructions/workflow.md`; worker role rules in `instructions/roles.md`.
- When a worker stops with a question, answer it with SendMessage to the same agent instead of starting a fresh one. Put standing answers for small ambiguities in the brief.
- Write briefs with a quoted heredoc (`<<'EOF'`); point workers at exact files, not whole dependencies.
- Probe and fixture-recording scripts record what the source returns and stop only on real errors (transport failure, non-2xx), never on valid but unexpected shapes such as nullable fields or an HTTP 200 with an error envelope.
- When a reviewer keeps finding variants of one edge case, close the class with one fail-closed rule in the design and enforce it in one function; collapse parallel code paths rather than patching each.
- Low-risk slices (read-only paths, decode fixes, docs) get one review round, and low findings are fixed without a separate confirmation; writes, security and release paths keep the full process.
- Keep briefs, reports and temporary source files in the scratchpad, outside the checkout. Go tools can scan `.go` files under gitignored `work/`. Findings not fixed at once become lines in `docs/plans/backlog.md`.
- Capture a check's exit status before filtering its log; `make pre-push | tail -1` can hide a failure.
- Run `make pre-push` before every push and the checks the brief names from `instructions/testing.md` on this MacBook. Run long jobs with `run_in_background`.
- Discuss with the owner in Vietnamese; write every file in English.

## Gotchas

- The Bash tool runs zsh: `$var` does not word-split; `"$sha:refs/heads/main"` is a history modifier, write `"${sha}:refs/heads/main"`.
- GNU tools come first on PATH: `sed -i`, not `sed -i ''`. Use `/usr/bin/grep` when `grep` is aliased to ugrep and a pattern fails with a complexity error.
- `gh` holds several accounts; for repository writes use `GH_TOKEN=$(gh auth token -u dannyota)`, and push with `git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push`, since the default helper answers as another account (403).
- Integration tests in a worktree: `ln -s ../trachuan/.env .env` (gitignored), run `make test-integration` (it loads `.env`), then remove the link; a bare `go test -tags integration` misses the database settings.
- Edited Markdown under `docs/` (not plans) must pass Prettier: run `make docs-fmt` or `npx prettier --write <file>` before `make pre-push`.
- For a reviewer diff that includes new files: `git add -N . && git diff > <scratchpad>/x.diff`.
