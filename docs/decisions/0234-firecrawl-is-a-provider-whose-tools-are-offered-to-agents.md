---
status: proposed
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0234. Firecrawl is a provider whose tools are offered to agents and may run on the operator's key

## Context and Problem Statement

The Firecrawl sample gives agents web search and page reading, but a person
must first find and import it, then paste a Firecrawl key, and then name its
functions in their agent. The operator defaults plan
([docs/plans/host-defaults.md](../plans/host-defaults.md), "Function
secrets") lets the operator pay for that key, and decision
[0232](0232-an-empty-key-on-an-operator-listed-provider-row-runs-on-the-operators-transport-under-a-daily-allowance.md)
left the rule for function secrets to this phase. A sample cannot carry an
operator key: an import rehomes it under the repository's authority as
`installed`, so the person can rewrite the code the key would reach
([0048](0048-providers-are-published-samples-are-copied.md)).

## Considered Options

- Keep Firecrawl a sample and inject the operator's key into any copy.
- Keep Firecrawl a sample and add the provider beside it.
- Move Firecrawl to the provider tier, offer its tools to agents, and inject
  the operator's key only into its published functions.

## Decision Outcome

Chosen: Firecrawl moves to the provider tier as
`providers.substrate.reamde.dev/firecrawl`, its two functions offer
themselves to agents, and the operator's key reaches only the functions the
defaults file names in that published package. This amends 0048, which
placed Firecrawl among the samples.

**The package.** It is the sample's closure under the provider authority,
installed `published`, with `scrapepage` renamed `webscrape` beside
`websearch`. The sample leaves the catalog. A copy a repository already
imported stays as it is, `installed`, with its own key and `scrapepage`.

**Offered tools.** Both functions declare `effect` (`read` for `websearch`,
`write` for `webscrape`) and `offer` (record
[0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)),
so an agent whose `toolsFrom` matches them, the seeded assistant among them,
takes them. Their tool names are declared, `web_search` and `web_scrape`,
not derived. A copy derives `firecrawl_websearch` when its owner offers it
and `websearch` when an agent names it under `tools:`, so the provider's
names cannot collide with a copy's and the copy cannot shadow them. The
plan's alternative, an install that reports the copy so the console can
offer its removal, is not needed while the names differ.

**The operator's key.** At invocation the engine sets
`config.settings.apiKey` to the operator's key only when the function's
package is listed under `functionSecrets` and stored `published`, the
function is one of that entry's `consumers`, the repository's own
`<package>/apiKey` secret has no value, and the allowance admits the entry's
`pricePerCall`. The call reserves that price before the body runs and
settles at it once the body may have started, whatever it answered, and at
nothing when the runner never started it; at the limit it is refused with
reason `quota`, never run on an empty key. A trigger of such a function is
held at the limit as an agent trigger is
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md)):
the dispatcher leaves the change or occurrence unclaimed, nothing parks, and
the first pass after the reset delivers it. An operator-paid
call gets the key alone in `config.settings`: no setting the person wrote
rides beside it. That is 0232's rule that nothing the person writes reaches
an operator-paid request, and it keeps the key off loopback: the bodies send
the key only to `https://api.firecrawl.dev` on 443, through an opener that
refuses redirects, unless a `baseUrl` setting names loopback, and only the
person can write that setting.

Refusing the operator's key whenever `baseUrl` is not the pinned origin was
the other way to hold that line, but the engine would then need each
provider's origin. Injecting into any copy lost because the repository can
edit a copy's code and read the key out of it. Keeping the sample beside the
provider lost because two packages would offer the same tools from one
catalog.

### Consequences

- Good, because a repository whose operator pays for Firecrawl searches and
  reads the web from its first chat, with nothing to import or name.
- Good, because the key reaches publisher code only, at one origin, and
  never a record, the sealed store, the changelog or an export.
- Good, because a person's own key still wins, and clearing it returns the
  calls to the operator's.
- Bad, because a repository holding the old copy sees two sets of web tools
  when its owner offered the copy's, and nothing removes the copy.
- Bad, because a provider's settings are dropped whole on an operator-paid
  call, so a later provider whose functions need a setting of the person's
  beside the operator's key needs this rule revisited.
- Bad, because a call settles at its price even when the body refused before
  sending anything, so the allowance overcounts those calls.

### Confirmation

`internal/engine/operatorfunction_db_test.go` holds the injection (listed
consumers only, the key alone, an own key winning, an empty one counting as
none, no copy, the allowance's refusal, reserve and settle at the price or at
nothing for a body the runner never started, a record and a schedule trigger
held until the reset, the read's `function/<package>` item) and the seeded
assistant taking both tools beside an offering copy. The engine and provider
test binaries confine every body to loopback, so a Firecrawl body these
tests run cannot reach Firecrawl.
`internal/providertest/firecrawl_test.go` holds the closure, the refused
origins and redirects, and `webscrape` behaving as `scrapepage` did;
`TestBodyHelpersFirecrawl` holds where the module sends a key that arrives
with no `baseUrl`.

## More Information

A later provider the operator pays for per call (speech, transcription)
follows the same rule: published, its consumers named, priced per call, and
the key alone in `config.settings`.
