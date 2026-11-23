# Classification Plan (Plan 8)

> **For agentic workers:** Routes: `instructions/workflow.md`. Executor: the primary. Paid calls use `ANTHROPIC_API_KEY` from `~/.config/claude-api-keys.env` or `OPENAI_API_KEY` from `~/.config/openai-api-keys.env`, passed per command, never exported, never switched while a batch is in flight.

**Goal:** every primary article in the release carries subject nodes: seeds from the 2023 codification package, then inheritance through consolidations, then an LLM layer in batches for the rest, gated by the floors in [taxonomy](../design/taxonomy.md#evaluation-gate).

**Budget:** the owner approved paid use on both keys on 2026-11-22 with no per-run approval; the 150 USD cap is lifted. Budget names and amounts on the whole ledger: pilot 15 USD (closed at 9.54), trial 200 USD, and `full`, which the primary sets before the full run as the ledger at that time plus the chosen trial's cost per article times the remaining articles plus 25 percent, adds to code, and raises when the ledger nears it (the standing approval covers a raise). Admission, reserves and settlement follow [classification results](../design/classification-results.md).

**Spec:** [classification results](../design/classification-results.md), [taxonomy](../design/taxonomy.md) ("Classification layers", "Serving", "Evaluation gate"), [batch submissions](../design/batch-submissions.md), [evaluation](../design/evaluation.md) filter gate. The spec wins over this plan.

## Measured size (corpus_vn_20261114_5, 2026-11-15)

| Item | Count |
| --- | --- |
| primary documents | 2,725 (445 consolidated) |
| primary articles | 23,688 |
| seeded by package ItemID and article number | 5,020 (21.2%) |
| unseeded in consolidated documents | 2,458 |
| unseeded elsewhere (LLM set before inheritance) | 16,210 in 1,663 documents |
| article text, embedder tokens | mean 649, p50 242, p95 2,263 |
| articles per chapter | 5.6 |

## Tasks

### 8.0 Design delta (primary, astra review)

- [x] taxonomy.md and ADR 0004: consolidation inheritance through corresponding articles (same anchor and heading), method `inherited` below seeds and above `llm` (data.md, corpus-schema.md), confidence 0.9, its own precision rows at levels 2, 3 and deepest; each layer gated by its own rows.
- [x] taxonomy.md: LLM request shape (chapters; groups of 12 without chapters), model, effort and prompt in the recipe, request size bounds, output keyed by article and validated against that article's candidates, `too_long` and `invalid_response` gaps; post-2023 slice model-adjudicated by Codex `gpt-6-astra`, blind, 20 read by the coordinator, then frozen with a hash; custom-node slice not applicable while `custom.yaml` is empty.

### 8.1 Seeds and inheritance in build (no paid calls)

- [ ] Migration adds `inherited` to the assignment method check. Build computes seed and inheritance assignments, with `seed_article_missing` gaps and the count of consolidated articles without a corresponding source; the inheritance rows and the filter gate run on them; seeds serve without floors, inherited assignments only when their rows pass.

### 8.2 Candidates and labeled set (no paid calls)

- [x] Labeled set from seeds: the 300 masked articles of taxonomy.md "Evaluation gate", drawn by `corpus taxonomy labels` from vn/20261115_3 (seed 1, 197 documents), frozen as `taxonomy-labels.json`.
- [x] Candidate selection: embedding and lexical candidates measured on the masked set fell far short of 0.95 (ADR 0004 History); taxonomy.md now limits each article to the subtrees of its document request's level-2 families and gates family recall at 0.95.
- [ ] Model-adjudicated sets (no Anthropic calls; Astra labels blind): inheritance (100 inherited consolidated articles) now; multi-family (100 unseeded articles, half from documents with a multi-topic document-level result) after the 8.4 screening pass, frozen before any multi-family scoring; post-2023 (100 articles). Multi-family and post-2023 frozen from vn/20261115_3 as taxonomy-multi-family.json and taxonomy-post-2023.json (gpt-6-astra, blind, under the labeling rule; the coordinator read 30 and then 20 items); inheritance unavailable (32 eligible). The coordinator reads 10 of multi-family and 20 of post-2023 before either gates.

### 8.3 Classify stage and batch submission

- [ ] Classify stage: request builder, Batch API submission per batch-submissions.md (intent log first, custom ids, polling, idempotent application, cost recorded per batch), result validation, thresholds calibrated per level on the labeled set.

### 8.4 Pilot (paid, small)

- [x] Confirm current Batch and model prices on Anthropic's pricing page (2026-11-16, cited in the classify PRs).
- [x] Screening pass inside the pilot budget (400 documents, 0.36 USD, 374 with two or more families): document requests only, for at most 400 documents drawn at random from those holding unseeded articles; it feeds the multi-family draw and measures nothing.
- [x] Labeled set only, both `claude-haiku-5-5` and `claude-sonnet-5-5`; family recall 0.970 and 0.973; deepest recall stayed below 0.70 for both models and for a two-step trial, so the LLM layer's scope is level 2 (ADR 0004 History 2026-11-17). Pilot ledger 5.28 USD.
- [x] Level-2 run of Haiku v2 and Sonnet v2 on the 200 model-adjudicated articles (2026-11-17, 3.93 USD, ledger 9.21; no fragments in either set). Level 2, multi-family / post-2023: Haiku recall 0.809 / 0.861, top-1 0.760 / 0.867, pair precision 0.592 / 0.641, family recall 0.852 / 0.889; Sonnet recall 0.809 / 0.852, top-1 0.780 / 0.876, pair precision 0.547 / 0.626, family recall 0.826 / 0.880. Additional-node precision over both sets: Haiku 25/104 (0.240), Sonnet 22/120 (0.183), floor 0.80. Seeded slice, all 300 / non-fragment 260 (40 fragments), level 2: Haiku v2 recall 0.873 / 0.881, top-1 0.786 / 0.791; Sonnet v2 0.920 / 0.935, 0.810 / 0.813. No row passes: family recall caps recall (given the family, level-2 recall is 0.95 to 0.98), and additional nodes are mostly wrong; Sonnet is no better than Haiku at level 2.
- [x] Post-2023 and multi-family references: Codex `gpt-6-astra` labels them blind under the labeling rule, the coordinator reads samples, and they are frozen (`corpus taxonomy freeze`).
- [x] Prompt-4 trial, Haiku (2026-11-17, 0.33 USD): each article request chose from the full level-2 menu, returning one primary and at most one additional node. Raw level 2, seeded non-fragment / multi-family / post-2023: recall 0.815 / 0.765 / 0.815, top-1 0.687 / 0.730 / 0.770, all below v2; additional 26/106. Top-1 0.90 needs a primary threshold of 0.96, which leaves 99 to 100% of articles unassigned; no additional threshold reaches 0.80. The split-half check holds as a fail (recall 0.00 to 0.07 in both directions). Not merged.
- [x] Pilot ledger closed at 9.54 of 15 USD. No further paid prompt trials: the LLM layer serves nothing, and level 2 comes from seeds and overrides only.
- [x] Free offline nearest-neighbour experiment (2026-11-17, no API cost). Each article took the level-2 code its nearest seeded articles agreed on, using vn/20261115_3 embeddings, with no neighbour from the same or a related document; k and the agreement rule were chosen on one half. At full coverage, top-1 was 0.66 on the seeded 260, 0.63 on multi-family and 0.70 on post-2023. Top-1 0.90 on the seeded half needs abstention down to 35 to 40% coverage (recall 0.32 to 0.34). Held-out halves: seeded 0.863 and 0.911, multi-family 0.846 and 0.842, post-2023 0.903 and 0.821, at coverage 0.35 to 0.62. Fails the level-2 floors; no design proposal. Allowing same-document seeded neighbours raises the seeded slice to 0.96 (leakage there), while adjudicated articles reach only 0.70 to 0.75.

### 8.5 Full batch (paid, after the coordinator confirms the estimate)

- [ ] Superseded by 8.6: unassigned gaps below threshold; evaluated corpus run; release.

### 8.6 Newest models (reopened 2026-11-22)

Order: land the OpenAI provider and freeze both independent references before the paid trials. The budget paragraph above names Anthropic's budgets only.

- [x] Classify accepts `claude-opus-5-5` and `claude-fable-5-2` at their Batch prices and the Anthropic `trial` budget (migration for the submission checks).
- [x] Result store ([classification results](../design/classification-results.md)): request key, immutable attempt records, the `final` rule, three attempts per key, the evidence rule with `.settled` files (pilot imported once), key locks across releases in one transaction with admission, settlement by token kind, replay; requests put the stable prefix first with cache markers; price table with all token kinds and a version.
- [x] Dropped by the owner (2026-11-23), never run: trial `claude-fable-5-2` (cache-ordered prompts, document then article requests, level 2) on the seeded 300 and the 200 model-adjudicated articles; score every slice; record the result in ADR 0004 History whether it passes or fails.
- [ ] OpenAI Batch provider on `/v1/responses` (metadata and input file id for matching, output and error files merged by `custom_id`).
- [ ] Verify the [OpenAI adapter](../design/classification-openai.md) and per-provider admission: project scope, count journal and count parity, Batch output and error shapes, and cache usage before starting the full trial population.
- [x] Decided by the primary under the owner's standing approval (2026-11-22): cap OpenAI at `openai-trial` 40 USD and `openai-full` 175 USD, both cumulative on the OpenAI ledger, leaving about 25 USD of the reported credit outside the cap. Keep Anthropic `trial` at 200 USD on its own ledger and `full` as above. Verify usable provider credit before spending; a cap is not a credit balance.
- [x] Decided by the primary: use the [blind Claude agent session](../design/taxonomy.md#evaluation-gate) for the frozen multi-family and post-2023 lists, not a paid adjudication command.
- [x] Dropped by the owner (2026-11-23), never run; replaced by the three-model trial below: run `gpt-6.2-astra` under `openai-trial` in parallel with the `claude-fable-5-2` trial under Anthropic `trial`. Use the same frozen article lists, document-then-article stages and level-2 scope. Score every applicable slice against the other provider's labels where adjudicated. Dry-run each ledger; split and settle batches if reserves limit admission. Record the OpenAI outcome in ADR 0004 History whether it passes or fails.
- [x] Evaluation-only trial command `corpus classify trial-openai` (all state under `--out`, 40 USD cap across sibling directories, never touches the workshop ledger or the submissions store).
- [x] Trial of `gpt-6.2-sol`, `gpt-6.1-terra` and `gpt-6-luna` on the seeded 300 and the two Claude-labeled sets (2026-11-23, 3.68 USD). Owner decision: `gpt-6.2-sol` for both stages, level-2 floors moved to its measured baseline, no `gpt-6.2-astra` (ADR 0004 History 2026-11-23, taxonomy.md floor table). A mixed recipe (Sol documents, Terra articles) was not run: the recipe ties the article model to the document model, and Terra alone already missed the old post-2023 precision floor.
- [x] Full run of `gpt-6.2-sol` over the LLM set of `vn/20261121_1` with `trial-openai --select unseeded:I/32` (2026-11-23): 64 batches settled, 28.53 USD; 1,541 documents (1 `no_family`), 14,631 articles, 14,452 with at least one code, 42 in a `no_family` document. Packed by `corpus classify pack` as set `72e6418f4817bc963c27ca7889740bf821c55418c8b91bee5c1feeba4d758bf9` (local artifact store). The release holds only seeded assignments, so unseeded articles of consolidated documents are included. One article batch was abandoned before Create after a transport failure on upload and rerun; the first 8-part attempt left about 0.7 USD of unused settled document batches.
- [ ] Pack the verified backfill into the artifact store, write unfiltered level-2 `llm` rows on exact text matches and gate publication in staged `eval` ([Publish](../design/classification-publish.md)); three slices: pack, freeze and write rows, gate and release.
- [ ] Before the next paid round: a selector that excludes recorded article triples and reuses exact document requests; count tokens concurrently with connection reuse (the article stage makes about 800 sequential count calls per part, 197 s measured). Production Batch runner wiring is deferred; the trial archives are not imported into the workshop ledger.
- [ ] Pick the winner by [Providers and models](../design/classification-results.md#providers-and-models). Estimate its remaining cost from the trial cost per article, with 25 percent headroom. Check the approved full cap against the winner's ledger plus that estimate and verify the provider credit covers the remaining spend and unresolved reserves. Owner approval: if either check fails, pause for funding or an approved cap change rather than switching models. If no model passes, publish no LLM assignments.
