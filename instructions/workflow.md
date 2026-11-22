# Workflow

The primary session plans, writes documentation, edits and decides designs and ADRs, briefs, verifies and owns Git. Sol drafts designs, Terra implements and troubleshoots bounded tasks, and a reviewer on a different model from the author reviews designs and code independently. The primary asks the owner only for decisions the owner must make, one at a time, with a recommendation. It answers worker questions from the reviewed design, settles conflicts itself with the reviewer's input, and records approved contract changes in `docs/design/` or an ADR before work resumes.

## Routes

|Work|GPT agent|Claude stand-in|
|-|-|-|
|Design, ADR or contract draft (role `designer`)|Sol|Opus|
|Design review (role `reviewer`)|Astra|Fable|
|Implementation and troubleshooting (role `implementer`)|Terra|Sonnet|
|Code review (role `reviewer`)|Sol|Opus|
|Searches, summaries, mechanical reading|-|Claude `Explore` agent|

The primary writes documentation and prose itself, edits every design draft and decides. Role rules are in `instructions/roles.md`; each brief names the role and points the worker there.

Dispatch each worker as a fresh `Agent` subagent with `subagent_type` `terra`, `sol` or `astra` and no `model` override. These are the owner's user-level agents; each alias resolves to the newest model of its family, so plans and briefs name the family, never a model ID. The worker's first report line is its runtime model ID, which must belong to the named family. When a user-level agent cannot be dispatched, use the Codex plugin (`codex:codex-rescue` agent for work, `/codex:review` or `/codex:adversarial-review` for review) with `--model` set to the newest ID of the family in `~/.codex/models_cache.json`; when GPT is unavailable, use the Claude stand-in with the `Agent` `model` `sonnet`, `opus` or `fable`. Never fork for independent review: a fork inherits the parent model and context.

A reviewer never runs on the author's model. Astra and Fable never write code. These agents have every tool, so a reviewer brief forbids edits, writing commands and Git. Give reviewers the exact diff, worktree status, local checks and exact-commit CI evidence. Resume the same reviewer with SendMessage to confirm fixes, and answer a worker's question with SendMessage to the same agent instead of starting a fresh one.

## Flow

1. Read the design and accepted ADRs for the area. An accepted ADR wins over design text; design wins over a plan.
2. For a contract or architecture change, Sol drafts the proposal from the primary's brief, the primary edits and decides, and Astra reviews it before any code. Documentation and prose the primary wrote need no independent review. Small changes skip design.
3. Brief one implementer per bounded task with exact owned paths. Failing test first, then the smallest fix. Adversarial cases for hostile input, authorization, races and bounds belong to the author.
4. The primary runs `make pre-push` and the checks the brief names, inspects the diff and report, then dispatches the independent reviewer once after the last code task. One-line and test-only changes skip review.
5. Return findings to the author; the same reviewer confirms the fixes.
6. Push a branch, open a draft pull request, mark it ready when complete. Landing needs independent review and green CI on the exact head; the primary then merges (feature PRs without `--admin`) and deletes the branch. A change only to agent setup or working process (`.claude/`, AGENTS.md, CLAUDE.md, `instructions/`) needs no independent review: the primary merges it after `make pre-push` with `gh pr merge --admin`, which the repository admin role may bypass on the `main` ruleset.
7. When CI fails, the primary saves the failing job logs to the scratchpad and briefs `implementer` with them. Two red runs from the same cause means stop and repair the process.

## Reviews

Security-relevant work beyond one line needs an adversarial pass before merge: login, sessions, OAuth, tokens, CIMD fetching, rate limits, corpus release activation, backups, deploy and release workflows. Model these attackers: an anonymous visitor; a signed-in user attacking another account; a hostile MCP client or CIMD document; a stolen token or cookie; a network position between client and edge; a compromised container on the host; a leaked backup.

Review output: Verdict (`merge`, `fix first`, `redesign`); Findings ordered blocker, major, minor as `R-nn. [severity] [category] file:line` for code, `S-nn.` for design, `A-nn. [attacker]` for adversarial; Checks read; Not checked. Quote the text, state the failure scenario, give one fix sentence. Do not write the fix.

## Briefs and reports

Follow AGENTS.md "Briefs and reports". Workers report an edit needed outside their paths instead of making it. Never hand-edit generated output; regenerate from the source and report every generated file changed.
