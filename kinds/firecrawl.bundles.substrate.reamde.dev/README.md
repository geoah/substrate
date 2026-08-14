# The Firecrawl bundle — web search + scraping as agent tools

A **capability bundle**: it gives the substrate's agents hands on the live
web — search and read — over the Firecrawl API. It is deliberately NOT an
account integration: one API key on the bundle config, no `accountconfig`, no
`oauth2`, no host OAuth flow. Where the Google bundle proved the connector
machinery, this one proves the _tool_ shape: a bundle whose whole surface is
two callable functions.

And no triggers. Both functions are pure callables (`mode: call`) — nothing
here subscribes to the changelog or a schedule, so the closure ships with
zero delivery wiring and installs from `bundle.yaml` alone. (Triggers were
never closure members anyway — they are ordinary data records in
`core.substrate.reamde.dev` — so a trigger-less bundle admits exactly like a
wired one.)

## What it does

```
websearch  {query, limit?}  ──▶  Firecrawl /v2/search  ──▶  {results: [{title, url, snippet}]}
                                                            (a READ tool — no effects, ever)

scrapepage {url}            ──▶  Firecrawl /v2/scrape  ──▶  {document, url, title?, content, truncated?}
                                       │                     (markdown, capped at 24000 chars)
                                       ▼
                          webdocument (put with host.ids.url(url))
                          re-scraping the same URL UPDATES the one document
```

- **`websearch`** (python) — calls Firecrawl search and returns the hits as
  `{title, url, snippet}`. Effects-free by construction: its `emit` names a
  type only because the manifest requires a non-empty allowlist — a ceiling,
  not a promise.
- **`scrapepage`** (python) — validates the `url` input FIRST (an absolute
  `https://` URL with a hostname and no embedded credentials — refused
  before it is hashed into a document id and before the provider is
  dialed), calls Firecrawl scrape (`formats: [markdown]`), caps the content
  at 24000 characters the way the v4 `web_fetch` tool did (`truncated:
  true` marks a cut), and keeps the page as a **`webdocument`** in this
  bundle's OWN authority — never another bundle's type (the web harvester's
  `page` stays the harvester's; a bundle's effects stay inside the authority it
  owns). The id is `host.ids.url(url)` with `if_absent: false`, so a
  re-scrape updates the one document instead of minting a second.
- **`webdocument`** — `url`, `content` (text), `truncated`, `fetchedAt`,
  `raw` (the scrape metadata, for provenance). `title` is the reserved
  built-in; scrapepage writes it.
- **`config`**, the `connector` input's kind: a secret-typed `apiKey`
  (`writer: owner`, injected only into this bundle's functions and scrubbed at
  the runner boundary, never read back) and an optional `baseUrl` (default
  `https://api.firecrawl.dev`).

### What `baseUrl` controls — and what it cannot

`baseUrl` is the API root both bodies call (`{baseUrl}/v2/search`,
`{baseUrl}/v2/scrape`). It exists for exactly two things: the production
Firecrawl API and a loopback fake in tests. Because the apiKey rides every
request and `baseUrl` is owner-editable, both bodies **origin-pin** it
before building a request: HTTPS to `api.firecrawl.dev`, or loopback
(`127.0.0.1` / `localhost` / `::1`, any scheme and port) as the test seam.
Any other value — another host, plain `http://` to a remote, an embedded
`user:pass@` — is refused with a clear error and nothing is sent. (The
declared `network` capability is not yet enforced by the same-host runner;
this bundle-side pin is the defense until host-side egress enforcement
lands.) A general proxy therefore cannot be configured here — that is
deliberate: a config edit must never be able to redirect the credential.

## Install and configure

```sh
substratectl apply -f bundle.yaml
# then create a config record carrying your Firecrawl API key
```

The config record is per-repository (the key is a credential), so it is not
shipped here. The bundle declares one input, `connector`, satisfied by a
`config` record: the sole record resolves on its own, several resolve through
the one named `default` or an explicit bind, and the bundle status reports an
unresolved input as a setup step until one lands. Keys come from
https://www.firecrawl.dev (they look like `fc-…`).

## Binding the tools to an agent

A function with an `arguments:` list is its own tool card: the engine renders the
card from the list, and an agent names the function in `tools:`, exactly the way
the web harvester's classifier carries `setclass`. A research agent that searches, reads and remembers:

```yaml
kind: core.substrate.reamde.dev/agent
metadata: {id: myagents.bundles.substrate.reamde.dev/researcher}
data:
  authority: myagents.bundles.substrate.reamde.dev
  description: Research a question on the live web and cite what was read.
  prompt: |
    Search with websearch, read the promising hits with scrapepage, and
    answer with the document ids you drew on.
  provider: default
  model: anthropic/claude-opus-5
  tools:
    - function: firecrawl.bundles.substrate.reamde.dev/websearch
    - function: firecrawl.bundles.substrate.reamde.dev/scrapepage
  budgets: {maxTurns: 8, maxToolCalls: 16, depth: 3}
  permissions:
    writes:
      # scrapepage writes webdocuments, and a child's effective emit is its
      # own ∩ the caller's, so the agent's ceiling must name the type or the
      # tool's put is refused.
      - firecrawl.bundles.substrate.reamde.dev/webdocument
```

Both functions are equally callable without an agent — the HTTP call API or
another function's `host.call` (gated by that caller's `permissions.call`).

## Files

- `bundle.yaml` — the schema closure, and the whole bundle: authority + bundle +
  config type + webdocument + the two functions. No `triggers.yaml` — there
  is nothing to wire.

## Tested

`engine/firecrawl_bundle_db_test.go` installs this closure from these very
files: the loader admits it (a `connector` input injected into functions,
no oauth2/accountconfig, both callables registered, display metadata
present), the
zero-trigger closure installs into a live repository, and both functions run
against a fake Firecrawl server — websearch answers hits and applies zero
effects; scrapepage caps the markdown, writes the webdocument, and a
re-scrape updates it in place. The review fixes are pinned too: a `baseUrl`
pointed anywhere but the pinned origin or loopback refuses before any
request is sent, and scrapepage rejects schemeless / non-https /
credential-bearing urls before hashing or dialing. Real Firecrawl calls
never run in tests.
