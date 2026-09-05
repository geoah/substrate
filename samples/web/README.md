# The URL-harvester bundle — the shipped conformance example

This is the substrate's end-to-end conformance example. It is a
**real, installable bundle** — not a test fixture — and it exists to prove the
one rule of the primitive set:

> If the URL-harvester chain needs a special workflow, connector, link, or
> reflection primitive, the core is still too specific.

It needs none. Everything here composes from exactly seven kinds:
**bundle · kind · trait · function · trigger · agent · llmprovider**.

## What it does

```
message ──findurls──▶ page(pending) ──fetchpage──▶ page(fetched)
                                                        │
                                        classify-on-page trigger
                                                        ▼
                              pageclassifier (agent) ──setclass──▶ page.class
                                                        │
                                          routes to sub-agent
                                                        ▼
                        readinglistagent (agent) ──propose──▶ recordpatchrequest
                                                        │
                                       owner accepts ──applyDiff──▶ page.saved

   weekly schedule ──▶ weeklyrollup (agent) ──propose──▶ digest recordpatchrequest
```

- **`findurls`** (python) walks every string property of the triggering
  record, extracts URLs, honors `denyDomains` from the injected `connector`
  input, and mints
  a pending `page` per allowed URL with **put-if-absent** — so re-feeding the
  same message re-mints nothing.
- **`fetchpage`** (python) turns a pending page into markdown and marks it
  fetched. The fetch is a deterministic **stub** — no network, so tests never
  touch the wire; a production build swaps the body for a real fetch behind the
  `firecrawlKey` secret.
- **`setclass`** (python) is the classifier agent's write hand: an agent has no
  generic patch built-in, so a direct property write is a function tool.
- **`pageclassifier`**, **`readinglistagent`**, **`weeklyrollup`** are agents —
  LLM loops referencing the seeded `default` llmprovider row, each naming its
  own `model` (opus, sonnet and haiku respectively). The
  classifier's writes name `recordpatchrequest` too, so the sub-agent's `propose`
  survives the emit ceiling (a child's effective emit is its own ∩ the
  caller's).
- **`stampconfig`** (python) is the emit ceiling's NEGATIVE proof. The
  reading-list agent carries it and declares `config` in its own writes, but when
  it runs as the classifier's sub-agent its effective emit narrows to the
  classifier's (`page` + `recordpatchrequest`), so a `stampconfig` call is
  refused and nothing lands — delegation can only narrow, never widen.

## Files

- `bundle.yaml` — the **schema closure**: the atomic install unit (package +
  bundle + config type + page type + four functions + three agents).
- `triggers.yaml` — the **delivery wiring** as ordinary data records.

## Install

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
# then create a config record carrying denyDomains + firecrawlKey
```

The config record is per-repository (deny list, secret key), so it is not
shipped here. The bundle declares one input, `connector`, satisfied by a
`config` record: the sole record resolves on its own, several resolve through
the one named `default` or an explicit bind, and until one resolves the bundle
status reports the unresolved input as a setup step.

## The gate

`./substrate/engine/harvester_conformance_db_test.go` installs
this bundle from these very files and drives the whole chain green, asserting
the two properties the throwaway prototype proved:

1. **put-if-absent** — re-feeding re-mints nothing.
2. **replay-from-zero is quiet in the data** — replaying every record trigger
   from seq 0 writes only run/thread/message ledger rows, never data.

…at a causal depth well under the cap of 16.

> One as-built shape differs from the prototype's sketch: the prototype's
> owner-approval freely minted a `note` record. The as-built decision machine
> applies an accepted `recordpatchrequest` via `applyDiff`, which **patches an
> existing target**, so the "note" is realized as a durable mark on the page
> (`saved: true`) rather than a new record. This is a shape adaptation, not a
> missing primitive — the chain still composes from the seven kinds.
