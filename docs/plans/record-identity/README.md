# Record identity: ten paths to URL-shaped authorities

[Issue #194](https://github.com/geoah/substrate/issues/194) asks whether an
authority may contain path segments. This directory is not a decision: it is
one proposal per path, a paragraph with examples each, and this report with
the pros and cons. The decision, when made, becomes a record in
[docs/decisions/](../../decisions/README.md) and settles against
[0014](../../decisions/0014-authorities-widen-only-outside-the-id-alphabet.md).

## The problem

Identity has two levels. A **kind** is an authority (who published it, a
DNS-style name) plus a name, unique within that authority. A **record** is a
kind plus an id, one string, unique within the kind. There is no first-class
record authority, but the id alphabet admits `/` and `.`, so some records
embed their own publisher inside the id by convention: a declaration's id is
a kind reference. One flat string therefore carries up to two authorities:

```
core.substrate.reamde.dev/function/firecrawl.bundles.substrate.reamde.dev/websearch
[kind authority          ][kind    ][record's own authority               ][record name]
```

That composed string lives in exactly three places: reference property
values, trigger callables, and declaration ids. The chain's frame, the
fold's ops and the edges table already carry kind and id as separate
fields, but those three places are inside hash-chained payloads, so their
bytes can never be rewritten. The string splits without a registry because
of two character facts: an authority always carries a dot and never a
slash, and a kind name carries neither. The wants: publish kinds from a URL
nobody owns DNS for (`github.com/geoah/vocab`), and group one domain by
path instead of by subdomain.

## Rejected up front: escape spellings

Three drafted proposals respelled the URL's slashes as a different string
inside the stored authority: percent-encoding (`github.com%2Fgeoah%2Fvocab`),
a reserved marker (`github.com!geoah!vocab`), and a hyphen escape
(`github.com--geoah--vocab`). All three are removed and stay rejected as a
family: the stored identifier is not the URL, so every surface that shows
or imports one owes a bidirectional mapping forever; the escape character
or sequence is spent for all time (Go's proxy already spends `!` on case,
Bazel had to migrate an ecosystem off its first marker `~`); a pasted
real-slash URL is undetectable and keeps its old-grammar meaning; and the
respelling answers neither want, it only re-letters the sprawl. Any future
proposal that replaces `/` with another string is this family and inherits
this rejection.

## The first constraint: the split

One flat string holding several slash-bearing, variable-length parts cannot
be split by inspection. Every path does one of four things: keep slashes
out of authorities (1, 9, 10, 11, 13), keep slashes out of ids (4), mark
the authority's end with something other than a lone `/` (12), or stop
composing or stop inspecting (5, 6, 7). GraphQL constrains nothing: kind
and id travel as two separate string arguments, so every proposal works
over GraphQL unchanged. REST constrains more than first claimed: spellings
that keep the authority one segment route on today's fixed-arity router
unchanged; 4 and 12 need a new variable-depth router first, after which a
record address parses registry-free (4's list-vs-get still asks the
registry); 5 and 6 leave a raw wire path ambiguous from both ends
(authorities and ids both carrying slashes), so they encode or ask the
registry.

## The second constraint: names move

A URL is a lease and the changelog is a freezer. GitHub namespaces are
recyclable and repojacked at scale (an Aqua Security sample extrapolated to
millions of exposed repos; GitHub's retirement mitigation has been bypassed
four times; VulnCheck found 15,000 repojackable Go module repos), domains
expire and resell (the `ctx` and `phpass` hijacks; 8,494 npm packages
hijackable through expired maintainer domains), and Deno, the ecosystem
that bet hardest on URL-as-identity, built JSR to retreat from it. Systems
that kept name-first identity retrofitted an indirection: Go's checksum
database, npm and JSR provenance attestations, AT Protocol's DID behind
every handle. Proposals 4, 5, 6 and 12 bake the lease into the freezer in
different shapes; 1 has the same disease slower (a dotted authority is also
a rented domain); 9 dates the claim, 11 moves it to one registry, 13 stores
a stable key and makes the URL a replaceable alias, and 10 has nothing to
churn.

## Shared by every URL-shaped path

- The first-label keyings move first: the GraphQL type prefix (every
  `github.com/...` kind would be `Github_...` today), the `bundle:<name>`
  actor and bundle-name uniqueness, the `connector:<label>` actor, and a
  callable's actor being its bare local name. Each moves to a hash of the
  full authority or a declared, collision-checked name.
- The grammar copies widen together: the Go validators, the console, both
  function SDKs, the generated decoders, the keyed-map pattern shipped to
  Postgres, and the `kinds/` embed glob.
- URL normalization must be specified once: host case (GitHub paths are
  case-insensitive, URLs are not), dot-segments, trailing slashes, default
  ports, Unicode and homoglyphs. Every rule this spec gets wrong is frozen
  into the chain.
- The change ships behind a vocabulary dialect rung, so an older server
  refuses the repository at its next open instead of misreading it.
- An authority long enough that its declarations cannot fit the 128-byte id
  cap is refused up front (or the cap moves).

## The proposals, side by side

Grammar (the string or its split changes):

| # | Proposal | A record's stored reference | Split needs | Stored data today | Name churn |
|---|----------|-----------------------------|-------------|-------------------|------------|
| [1](1-dotted-authorities.md) | Reaffirm dotted authorities | `tasks.substrate.reamde.dev/task/t1` | nothing new | untouched | unanswered |
| [4](4-slash-free-ids.md) | Raw URLs, one-segment ids | `github.com/geoah/vocab/note/n1` | right-side split | breaks: wipe or re-sign | unanswered |
| [12](12-double-slash-separator.md) | A `//` boundary | `github.com/geoah/vocab//note/n1` | a banned sequence | one-time `//` audit | unanswered |

Shapes (the reference model changes):

| # | Proposal | A record's stored reference | Split needs | Stored data today | Name churn |
|---|----------|-----------------------------|-------------|-------------------|------------|
| [5](5-structured-references.md) | Structured references | `{"kind": "github.com/geoah/vocab/note", "id": "n1"}` | nothing (no composing) | reference values migrate | unanswered |
| [6](6-registry-split.md) | Split against the registry | `github.com/geoah/vocab/note/n1` | the registry, forever | untouched | unanswered |
| [7](7-id-only-references.md) | Id-only references | `n1` | uniqueness, not parsing | reference values migrate | unanswered |

Trust (the want is answered outside the grammar):

| # | Proposal | A record's stored reference | Split needs | Stored data today | Name churn |
|---|----------|-----------------------------|-------------|-------------------|------------|
| [9](9-url-provenance.md) | URL provenance, dotted identity | `vocab.geoah.dev/note/n1` | nothing new | untouched | dated proof |
| [10](10-content-addressed-kinds.md) | Content-addressed kinds | `mfrggzdfmzts.cas/note/n1` | nothing new | untouched | nothing to churn |
| [11](11-registry-scopes.md) | Registry-granted scopes | `vocab.geoah.r.substrate.dev/note/n1` | nothing new | untouched | moved to one registry |
| [13](13-minted-publisher-keys.md) | Minted keys, verified aliases | `pk7f2q9x4kd3.pub/note/n1` | nothing new | untouched | alias is replaceable |

## Pros and cons

**1. Dotted (status quo).** Pro: zero work, zero risk; the grammar
Kubernetes groups, AT Protocol NSIDs, Maven groupIds and Java packages all
chose and kept. Con: both wants unmet; publishing means picking a DNS name
nothing verifies; the subdomain sprawl is permanent; a rented domain is
still mutable identity.

**4. Slash-free ids.** Pro: identifiers are literally URLs on every
surface; registry-free split; AT Protocol ships exactly this shape
(`at://authority/collection/rkey`, no `/` in record keys), as does
Kubernetes (every name one segment). Con: the only path that breaks stored
data, and the break is cryptographic (a re-mint re-signs history under the
current key); declaration ids become minted; a record's embedded publisher
needs a new home; REST list-vs-get needs the registry.

**5. Structured references.** Pro: the composition problem ceases to exist;
ids stay free; the store already speaks pairs everywhere but three places;
Maven's coordinates prove the model boring and durable. Con: reverses the
dialect-2 migration that just retired the pair; reference values migrate;
XML namespaces warn that every surface will demand a compact single-string
spelling anyway, and the compact spelling leaking into content was that
generation's interop bug; the raw wire path is ambiguous, so REST encodes
or asks.

**6. Registry split.** Pro: literal URLs, no shape change, no data
migration; Go resolves import paths this way. Con: Go pins the answer in
`go.mod` and never re-asks, and a changelog has no pin per reference; a
pruned kind leaves stored values unsplittable; longest-prefix re-parses
immutable bytes when a longer authority is declared later; every reader
needs the fold to parse anything.

**7. Id-only.** Pro: references get as short as they can get; composes with
4, 5 or 6; DIDs prove flat opaque ids work as a layer. Con: repository-wide
uniqueness is already false in the shipped tree (one id names a kind and a
bundle; every configured bundle wants a `default`); `former_ids` is keyed
per kind; forward references become uncheckable; AT Protocol kept the
collection segment even though DID plus key would suffice, because typed
references are what keep stored data auditable.

**9. URL provenance.** Pro: both wants met with zero grammar work; the
proof (a well-known file naming the authority, as Go vanity imports do) is
dated and re-checkable; composes with every other proposal. Con: identity
is still first-come dotted names; the proof is only as live as the URL; the
want is met by convention plus one install-time check, which may
underwhelm the "identifiers are real URLs" ambition.

**10. Content-addressed.** Pro: unforgeable, unsquattable, nothing to
churn; the right primitive for pinning and verify. Con: identity changes
with every edit, so the upgrade model (stable identity plus integer
version) needs a lineage identifier on top, which reintroduces naming;
included so it is rejected on the record, not re-proposed.

**11. Registry scopes.** Pro: the retreat position JavaScript actually took
(JSR after Deno's URL imports); zero grammar work; publishing needs a scope
claim, not a domain. Con: somebody runs the registry; it becomes a trust
root and the system's first global namespace; the substrate today has no
central anything, and this adds one.

**12. `//` boundary.** Pro: the URL stays verbatim in the identifier, no
character respelled, and it splits by inspection (only the first `//` is
structure, so declaration ids stay kind references); Bazel proves the shape
at scale. Con: a sequence ban 0014's character rule does not cover; a
one-time audit confirms `//` is unclaimed in stored ids; the split changes
in every grammar copy, and REST needs a variable-depth router.

**13. Minted keys, verified aliases.** Pro: the only proposal that answers
name churn outright; history stores a stable key, so renames, transfers,
expiry and homoglyphs cannot rewrite or orphan it; AT Protocol, Go's sumdb
and npm/JSR provenance all converged here after shipping name-first. Con:
the biggest model change of the set: a publisher record and a verification
flow become part of the substrate; every human-readable display is a
lookup; the readable name stops being the identity, which is exactly the
point and exactly the cost.

## Getting to an ADR

The lanes compose: a grammar (1, 4, 12), a reference shape (5, 6, 7), and a
trust answer (9, 10, 11, 13) are mostly independent choices, and plausible
bundles include 1+9 (smallest), 12+9, 4+13 (the most literal URLs with the
strongest identity), or 1+11. Whatever is chosen becomes a decision record
that supersedes or extends 0014, inherits the shared obligations above, and
answers four questions every proposal must answer: what a declaration's id
looks like, where a record's own publisher lives, what happens to a stored
reference whose kind is later removed, and what happens when the URL stops
meaning its publisher.

## Prior art, in one place

- Go modules: URL-shaped paths whose leading element must carry a dot;
  module-vs-package boundary resolved by asking servers, then pinned in
  `go.mod`; uppercase escaped as `!x` in the proxy; 15,000 repojackable
  module repos found by VulnCheck despite the checksum database.
- OCI and Docker: first component is a registry iff it carries `.` or `:`
  (the same dot heuristic); tag and digest split from the right with
  reserved characters; the registry-dependent short-name corner is where
  the squatting CVEs grew.
- Kubernetes: API groups are DNS subdomains, `/` banned by design, every
  name one path segment; a decade of proposal 1 plus proposal 4's arity.
- purl: fixed separator order parsed right-to-left, percent-encoding inside
  segments only; years of spec issues over exactly when encoding applies.
- Deno and JSR: raw-URL identity shipped, hurt (verbosity, duplicate
  versions, dead hosts, registry-vs-package ambiguity), and was retreated
  from into registry-granted scopes.
- AT Protocol: `at://authority/collection/rkey` with slash-free record
  keys; handles are DNS names, identity is a DID underneath, verified both
  ways, precisely because domains churn.
- Bazel: three different separators (`@`, `//`, `:`) so every part may
  carry slashes; canonical vs apparent repo names; its first marker (`~`)
  had to be replaced ecosystem-wide by `+`.
- XML namespaces: URIs as authority names, compared byte-wise; every
  surface grew a prefix alias, and aliases leaking into content became the
  signature interop bug.
- RFC 4151 tag URIs: authority plus mint date, the registry-free answer to
  authorities changing hands over time.
