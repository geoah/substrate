---
name: docs-review
description: Verify and clean up the docs (docs/*.md and README.md). Use when asked to review, tighten, fix or check the documentation, or before merging a change that touches docs/. Runs the repo's own `mise run lint:docs`, then a reading pass against the rules no linter can hold, then verifies every factual claim against the code.
---

# Docs review

The docs describe the system as built, and the code is the contract: where a
page and the code disagree, the code is right and the page changes. This skill
is how a docs pass is run so the result is short, accurate and consistent.

## 1. The linter first

Fix everything it flags before reading anything:

```bash
mise run lint:docs
```

This skill owns no checks of its own. `lint:docs` is the one docs linter, it
is what CI's lint job runs, and it holds both halves: links and anchors
(lychee, offline) and the prose rules a grep can decide (`.mise/docscheck.sh`
— the dead words `docs/terms.md` retired, retired envelope keys, `mise run`
names that no longer exist, a page the index does not list). A rule worth
enforcing every time belongs in that script, not here.

## 2. The reading pass

Read each page as a first-time reader. Every sentence must parse on the first
read; precision stays, cleverness that costs comprehension goes. Fix, do not
merely note:

- **No load-bearing fragments.** A fragment may punctuate ("Registration
  seeds `core` and nothing else."), it may not carry the content. "Write. A
  file, applied." is the failure mode: rewrite as a complete sentence that
  says who does what.
- **A metaphor is defined where it first appears, or cut.** "Door", "hat",
  "fold", "seed", "admission" earn their keep only if the page (or a page
  earlier in the reading order) says what they stand for.
- **One concept, one owner page.** The envelope belongs to the data model,
  registration to getting started, the changelog contract to its own page.
  Everywhere else is one sentence and a link, not a re-explanation.
- **terms.md is the vocabulary.** One word per thing; a concept that goes by
  two names in two pages is a bug in one of them. The nuanced dead senses the
  linter cannot grep are caught here: type, schema, group, log and extension
  used where kind, vocabulary, authority, changelog or bundle is meant, and
  capability anywhere outside a function's `capabilities` envelope, a
  capability bundle, or a Linux capability.
- **Examples run on a fresh substrate.** Registration seeds `core` only, so a
  snippet that touches any other collection installs its bundle first.
  `get -o yaml` output must be directly `apply -f`-able.

## 3. Facts against code

Any claim that names a route, flag, field, task, env var, kind or property is
checked against the code before it survives:

- routes and payload fields: `internal/api`
- engine behavior (fold, apply/merge, watch resume, bundles, inputs):
  `internal/engine`, with `internal/substrate` as the contract
- CLI commands, flags and config shape: `cmd/substratectl`
- kind declarations, properties, states, versions: `kinds/`
- tasks and dev workflow: `.mise.toml` and `.mise/dev.sh`

A claim that cannot be verified is removed or rewritten to what the code
actually does; never "fixed" by guessing.

## 4. What not to do

- Do not rewrite for taste. A sentence that parses, is accurate and is not
  redundant stays as written, whatever its style.
- Do not touch code blocks except to make them correct.
- Do not add filler ("In this section we will..."), summaries of what a page
  just said, or content the index already carries.
- Do not edit `CLAUDE.md`: it is a symlink to `AGENTS.md`; edit `AGENTS.md`.
- Do not touch `docs/decisions/`. A decision record is history, not the system
  as built, so it is held to neither the vocabulary nor the code as they stand
  today; the `adr` skill owns it, and a record whose decision was reversed is
  superseded rather than corrected.

Finish by running `mise run lint:docs` once more and reporting: files changed,
findings fixed by category, and any claim you could not verify against the
code.
