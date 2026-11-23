# ADR 0004: Subject taxonomy seeded from Bộ pháp điển

Status: accepted 2026-11-08.

## Context

Agents need to narrow a search by subject before retrieving. VBPL classifies
only at document level. Bộ pháp điển (45 chủ đề, 271 đề mục, with
phần/chương/mục inside each đề mục) places every in-force article in a tree, but
its public portal now requires authentication; a 2023 official offline package
exists with 86,388 placed articles linked to VBPL ItemIDs.

## Decision

Levels 1 to 4 of the subject tree come from the 2023 codification package,
keeping names and codes. Custom nodes are declared in a YAML file for subjects
the codification lacks. Articles matching the package receive seed labels; an
article of a consolidated document whose corresponding source article is seeded
inherits those labels; the rest are classified by an LLM in batches (document,
then chapter with all its articles), each article choosing only within the
level-2 families its document request returned; manual overrides win. The
subject filter never hides results: search runs over the whole corpus and
reports the count outside the filter.

## Consequences

Official structure with little hand work; a ground-truth set to measure the
classifier. The package is from 2023, so post-2023 law depends on the LLM layer.
Pháp điển data is never served to users.

## Alternatives rejected

A hand-written tree. Pure embedding zero-shot classification. A fine-tuned
classifier. Hard-cut subject filtering.

## History

- 2026-11-08: accepted in the brainstorming conversation.
- 2026-11-08: after design review findings S-08 to S-11, fixed the precedence
  (manual over seed over llm), moved classification after embedding, and defined
  the import mapping and code rules in design/taxonomy.md.
- 2026-11-09: join spike ([research note](../research/phapdien-join-2026-11.md))
  found seeds for 25.3% of primary corpus articles (24.7% through the VBPL
  ItemID alone) and 91.6% of chủ đề 22 package articles; added Tiểu mục
  flattening, STT collision and empty đề mục rules to the import mapping.
- 2026-11-15: added consolidation inheritance (method `inherited`, below seeds,
  above `llm`) with its own evaluation rows after the classification design
  review; see design/taxonomy.md.
- 2026-11-16: replaced embedding-selected candidate nodes with the subtrees of
  the document request's level-2 families. On the 300 masked seeded articles of
  `vn/20261115_2`, whole-tree cosine top 10 reached candidate recall 0.05, 0.24
  and 0.37 at levels 2, 3 and deepest; the best variant (5 families of 20 nodes
  with ancestor closure) 0.82, 0.53 and 0.47; lexical and fused selection less.
  None came near the 0.95 floor, which now applies to family recall.
- 2026-11-16: corrected the seeded slice's precision after the Haiku pilot on
  the 300 masked articles failed every floor (all-pairs precision 0.569, 0.353
  and 0.260; recall 0.850, 0.653 and 0.560 at levels 2, 3 and deepest). Seeds
  place each article once while the design lets an article take several nodes,
  so all-pairs precision counted correct additional nodes as errors. The slice
  now gates recall and top-1 precision at the same floors; precision of
  additional nodes is gated on the model-adjudicated multi-family set (0.80).
  This is a correction, not a relaxation: recall alone already fails the pilot.
  The document menu now shows `<level-1> > <level-2>` names and omits level-2
  nodes with no provisions and no children (54 of 282), which removes all four
  duplicated level-2 names, including the empty 34.10 that took 34.11's
  documents.
- 2026-11-17: reduced the LLM layer's scope to level 2. On the 300 masked seeded
  articles (prompt v2), deepest-level recall was 0.583 for Haiku, 0.633 for
  Sonnet and 0.650 for a two-step request that chose within one family's
  subtree, all below 0.70; it reached 0.704 only when the family was already
  right. This is a scope change, not a floor change: the floors are unchanged,
  level 3 joins only when it passes them on every applicable slice, and deeper
  nodes come only from seeds and inheritance. The two-step request is retired:
  its family step lost families (recall 0.923 against the document request's
  0.970) and chose several for 241 of 300 articles.
- 2026-11-17: the LLM layer skips fragments, and evaluation scores
  non-fragments. A fragment (an article whose text only states effect,
  implementation, repeal or amendment) has no subject of its own: the package
  places it by its document, and its text gives a classifier nothing to judge.
  The seeded slice held 40 such articles of 300, so scoring them measured a case
  production never sends. Every report still states the all-entries numbers
  beside the non-fragment ones.
- 2026-11-17: the LLM layer serves nothing for now. A trial in which each
  article request chose from the full level-2 menu, returning one primary and at
  most one additional node, did worse than the document-restricted prompt on
  every slice (seeded top-1 0.687 against 0.791). Top-1 0.90 needed a confidence
  threshold of 0.96, which left 99 to 100 percent of articles unassigned. A
  split-half check gave the same result in both directions. Level 2 comes from
  seeds and overrides only, as the design already allows, until a classifier
  passes the floors with real coverage.
- 2026-11-22: the LLM layer reopens with the newest models of both providers.
  The pilot used the cheapest models to stay inside a 150 USD cap, and Opus was
  never tried; the owner lifted the cap. Classification is a one-time backfill
  plus a few new documents a day, so quality outweighs price, provided no paid
  result is computed twice: every result is stored by request key and reused,
  and calls stay in the corpus write path
  ([classification results](../design/classification-results.md)). The floors
  are unchanged.
- 2026-11-23: the level-2 floors move to the measured baseline of `gpt-6.2-sol`,
  and it classifies both stages. The owner dropped the `claude-fable-5-2` and
  `gpt-6.2-astra` trials and chose among OpenAI models, quality before price,
  because each document is classified once and its result is reused. On the
  seeded 300 and the two Claude-labeled sets of 100 (level 2, seeded
  non-fragment / multi-family / post-2023): `gpt-6.2-sol` recall 0.915 / 0.871 /
  0.865, pair precision 0.728 / 0.711 / 0.726, top-1 0.846 / 0.859 / 0.845,
  family recall 0.960 / 0.914 / 0.904; `gpt-6.1-terra` recall 0.858 / 0.802 /
  0.837, precision 0.782 / 0.823 / 0.798; `gpt-6-luna` recall 0.835 / 0.767 /
  0.827, precision 0.781 / 0.802 / 0.811. The three trials cost 3.68 USD. No
  model passed the old floors, and neither did Haiku or Sonnet. Dropping
  additional nodes or raising their confidence threshold did not help: primary
  nodes alone lost 10 to 22 points of recall and stayed below 0.85 precision.
  The new floors are recall 0.85, top-1 precision 0.80, pair precision 0.70 and
  family recall 0.90, all at level 2. Recall leads because an agent uses a
  subject as a filter, where a missing node hides an article and an extra node
  only widens the result. The deeper rows and the inheritance rows are
  unchanged.
- 2026-11-23: a blind recheck suggests the references miss labels, so measured
  pair precision understates `gpt-6.2-sol`. Four `gpt-6.2-sol` agent sessions
  judged every level-2 pair that either the reference or the classifier gave for
  the 200 Claude-labeled articles, with the sources hidden. They accepted 161 of
  191 pairs both sides gave, 44 of 75 pairs only the classifier gave and 11 of
  29 pairs only the reference gave. Against those verdicts the classifier's
  precision is 0.78 and the reference's is 0.80. The judge shares the
  classifier's model and saw only proposed pairs, so this is evidence, not a
  gate: the floors stay measured against the frozen references, which are not
  amended from classifier output. A higher precision floor needs an
  owner-labeled set.
- 2026-11-23: serve every validated level-2 code without a confidence cutoff.
  Pruning the `gpt-6.2-sol` baseline loses recall and does not reach the former
  0.85 top-1 target. The settled OpenAI trial directories are the paid record
  for [publication](../design/classification-publish.md); the Batch runner
  wiring for OpenAI is deferred. The floors and the write-path restriction are
  unchanged.
- 2026-11-23: a second blind recheck by two judges that are neither the
  classifier nor each other finds the references incomplete rather than wrong.
  `gpt-6.2-astra` and `claude-fable-5-2` agent sessions each judged all 698
  level-2 pairs that the reference or the classifier gave for the 500 reference
  articles, sources hidden; they agreed on 549. Both accepted 385 of 462 pairs
  that reference and classifier share, 86 of 178 pairs only the classifier gave
  (both rejected 37) and 12 of 58 pairs only the reference gave (both rejected
  21). Counting a pair as right only when both judges accept it, the
  classifier's pair precision is 0.74 and the reference's is 0.76; counting it
  as wrong only when both reject it, 0.93 and 0.95. Of the pairs both judges
  accept, the classifier gave 97 percent and the reference 82 percent. The
  judges saw only proposed pairs, so recall is an upper bound. On
  `vn/20261121_1` the floors of that morning also failed by one article
  (post-2023 family recall 93 of 104 against 0.90, one reference no longer
  matching), which showed they had no margin.
- 2026-11-23: the two Claude-labeled references are corrected by that consensus
  and the level-2 floors are set with a margin. The owner chose to fix the
  reference rather than lower the floors to fit an incomplete one. Remove a
  label both judges reject, add a level-2 node both accept, leave a split
  unchanged; each 100-article set gained 16 pairs and lost 5, and one post-2023
  article kept no label. The 300 seeded articles keep the official package
  placements. A Claude judge had to agree to every change, so the corrected sets
  stay independent of the OpenAI classifier, and they gate only `gpt-6.2-sol`
  ([reference correction](../design/classification-publish.md#reference-correction)).
  The settled predictions are reused by exact article identity, with no new paid
  run. Measured on `vn/20261121_1`, non-fragment level 2: seeded recall 238 of
  260, top-1 220 of 260, family recall 250 of 260; multi-family recall 116 of
  127, pair precision 116 of 142, family recall 120 of 127; post-2023 recall 105
  of 115, pair precision 105 of 122, family recall 108 of 115, one reference no
  longer matching. Floors: recall 0.85, top-1 precision 0.80, pair precision
  0.75, family recall 0.90, each four to seventeen articles below the measured
  counts. The review covered proposed pairs only, so these sets measure
  regression of this classifier, not recall over subjects nobody proposed.
