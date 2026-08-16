# Record identity: seven paths to URL-shaped authorities

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
bytes are never rewritten in place. The string splits without a registry
because of two character facts: an authority always carries a dot and never
a slash, and a kind name carries neither.

## The requirements

- **The wants.** Publish kinds from a URL or DNS name the publisher
  actually controls, without renting a dedicated zone, and group one
  domain's vocabularies by path instead of by subdomain.
- **Authorities are verifiable.** An authority is a DNS name or URL whose
  control is proven when it is claimed: a DNS record or a well-known file
  served over HTTPS (ACME's DNS-01 and HTTP-01 are the two shapes). The
  installer checks the proof and records what was checked and when. An
  unverifiable authority is not installable.
- **Breakage is accepted.** Existing installations and repositories may be
  reset for this change. Stored-data compatibility still prices a proposal;
  it no longer gates one.

## Rejected

- **Escape spellings** (percent-encoding `github.com%2Fgeoah%2Fvocab`, a
  reserved marker `github.com!geoah!vocab`, a hyphen escape
  `github.com--geoah--vocab`): the stored identifier is not the URL, so
  every surface owes a bidirectional mapping forever; the escape character
  is spent for all time (Go's proxy already spends `!` on case, Bazel had
  to migrate an ecosystem off `~`); a pasted real-slash URL is undetectable;
  and the respelling answers neither want. Any future proposal that
  replaces `/` with another string is this family and inherits this
  rejection.
- **Registry-granted scopes** (JSR's `@scope` shape): one hosted namespace
  granting names is too centralized for a system whose identity is
  otherwise per-repository; the registry becomes a trust root and an
  availability dependency for every name claim.
- **Content-addressed kinds** (the authority is a hash of the declaration):
  unforgeable and unsquattable, but unverifiable in the required sense
  (there is no DNS entry or well-known file behind a hash), and identity
  changes with every edit, so the upgrade model needs a lineage name on
  top, which reintroduces the naming problem. Still the right primitive for
  pinning and verify, as OCI digests and go.sum show.
- **The unverified status quo** (reaffirm dotted authorities, change
  nothing): fails the verifiability requirement by definition. The dotted
  grammar survives only as proposal 9, which adds the proof.

## The first constraint: the split

One flat string holding several slash-bearing, variable-length parts cannot
be split by inspection. Every surviving path does one of four things: keep
slashes out of authorities (9, 13), keep slashes out of ids (4), mark the
authority's end with something other than a lone `/` (12), or stop
composing or stop inspecting (5, 6, 7). GraphQL constrains nothing: kind
and id travel as two separate string arguments, so every proposal works
over GraphQL unchanged. REST constrains more than first claimed: 9 and 13
route on today's fixed-arity router unchanged; 4 and 12 need a new
variable-depth router first, after which a record address parses
registry-free (4's list-vs-get still asks the registry); 5 and 6 leave a
raw wire path ambiguous from both ends (authorities and ids both carrying
slashes), so they encode or ask the registry.

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
every handle. Verification narrows this but does not close it: a proof is
a dated fact, and the domain or repo can change hands after it. Proposals
4, 5, 6, 9 and 12 store the verified name and live with that; 13 stores a
stable key and makes the name a replaceable alias, the only outright
answer.

## Shared by every path

- **Verification mechanics are one spec**: a DNS record or a well-known
  HTTPS file naming the authority, checked at claim time by the installer,
  recorded with its date on the bundle. For a URL authority the well-known
  file lives at the URL itself (a file in the repo); for a dotted authority
  it lives at the domain.
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
- The change ships behind a vocabulary dialect rung, so a stale binary
  refuses a newer repository by name instead of misreading it, even in a
  world where resets are acceptable.
- An authority long enough that its declarations cannot fit the 128-byte id
  cap is refused up front (or the cap moves).

## The proposals, side by side

Grammar (what the identifier looks like):

| # | Proposal | A record's stored reference | Split needs | Verification | Name churn |
|---|----------|-----------------------------|-------------|--------------|------------|
| [9](9-verified-dotted-authorities.md) | Verified dotted authorities | `vocab.geoah.dev/note/n1` | nothing new | DNS record or well-known file at the domain | dated proof |
| [4](4-slash-free-ids.md) | Raw URLs, one-segment ids | `github.com/geoah/vocab/note/n1` | right-side split | well-known file at the URL | dated proof |
| [12](12-double-slash-separator.md) | A `//` boundary | `github.com/geoah/vocab//note/n1` | a banned sequence | well-known file at the URL | dated proof |

Shapes (how a reference is stored; each composes with a grammar):

| # | Proposal | A record's stored reference | Split needs | Verification | Name churn |
|---|----------|-----------------------------|-------------|--------------|------------|
| [5](5-structured-references.md) | Structured references | `{"kind": "github.com/geoah/vocab/note", "id": "n1"}` | nothing (no composing) | per its grammar lane | per lane |
| [6](6-registry-split.md) | Split against the registry | `github.com/geoah/vocab/note/n1` | the registry, forever | per its grammar lane | per lane |
| [7](7-id-only-references.md) | Id-only references | `n1` | uniqueness, not parsing | per its grammar lane | per lane |

Trust (identity survives the name):

| # | Proposal | A record's stored reference | Split needs | Verification | Name churn |
|---|----------|-----------------------------|-------------|--------------|------------|
| [13](13-minted-publisher-keys.md) | Minted keys, verified aliases | `pk7f2q9x4kd3.pub/note/n1` | nothing new | alias proven, key stored | answered: alias replaceable |

## Pros and cons

**9. Verified dotted authorities.** Pro: the smallest change that meets the
verifiability requirement; zero grammar work; the grammar Kubernetes
groups, AT Protocol NSIDs and Maven groupIds all kept; a pages domain
(`geoah.github.io`) counts, so no zone rental. Con: the path-grouping want
stays unmet (grouping is still subdomains); the proof is only as durable as
the domain.

**4. Slash-free ids.** Pro: identifiers are literally URLs on every
surface; registry-free split; AT Protocol ships exactly this shape
(`at://authority/collection/rkey`, no `/` in record keys), as does
Kubernetes (every name one segment); the reset it needs is accepted by the
requirements. Con: declaration ids become minted and their identity moves
to a property; a record's embedded publisher needs a new home; REST
list-vs-get needs the registry and a new variable-depth router.

**5. Structured references.** Pro: the composition problem ceases to exist;
ids stay free; the store already speaks pairs everywhere but three places;
Maven's coordinates prove the model boring and durable. Con: reverses the
dialect-2 migration that just retired the pair; XML namespaces warn that
every surface will demand a compact single-string spelling anyway, and the
compact spelling leaking into content was that generation's interop bug;
the raw wire path is ambiguous, so REST encodes or asks.

**6. Registry split.** Pro: literal URLs, no shape change; Go resolves
import paths this way. Con: Go pins the answer in `go.mod` and never
re-asks, and a changelog has no pin per reference; a pruned kind leaves
stored values unsplittable; longest-prefix re-parses immutable bytes when a
longer authority is declared later; every reader needs the fold to parse
anything.

**7. Id-only.** Pro: references get as short as they can get; composes with
4, 5 or 6; DIDs prove flat opaque ids work as a layer. Con: repository-wide
uniqueness is already false in the shipped tree (one id names a kind and a
bundle; every configured bundle wants a `default`); `former_ids` is keyed
per kind; forward references become uncheckable; AT Protocol kept the
collection segment even though DID plus key would suffice, because typed
references are what keep stored data auditable.

**12. `//` boundary.** Pro: the URL stays verbatim in the identifier, no
character respelled, and it splits by inspection (only the first `//` is
structure, so declaration ids stay kind references); Bazel proves the shape
at scale. Con: a sequence ban 0014's character rule does not cover; a
one-time audit confirms `//` is unclaimed in stored ids; the split changes
in every grammar copy, and REST needs a variable-depth router.

**13. Minted keys, verified aliases.** Pro: the only proposal that answers
name churn outright; history stores a stable key, so renames, transfers,
expiry and homoglyphs cannot rewrite or orphan it; the alias claim is
verified by exactly the required mechanism; AT Protocol, Go's sumdb and
npm/JSR provenance all converged here after shipping name-first. Con: the
biggest model change of the set: a publisher record and a verification flow
become part of the substrate; every human-readable display is a lookup; the
readable name stops being the identity, which is exactly the point and
exactly the cost.

## Getting to an ADR

The lanes compose: a grammar (9, 4, 12), optionally a reference shape (5,
6, 7), and optionally the trust indirection (13). Plausible bundles: 9
alone (smallest), 4+13 (the most literal URLs with the strongest identity),
12 alone (verbatim URLs with composed references intact). Whatever is
chosen becomes a decision record that supersedes or extends 0014, inherits
the shared obligations above, and answers four questions: what a
declaration's id looks like, where a record's own publisher lives, what
happens to a stored reference whose kind is later removed, and what happens
when the URL stops meaning its publisher after its proof was recorded.

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
  name one path segment; a decade of dotted groups plus proposal 4's arity.
- ACME (HTTP-01, DNS-01): the two proof shapes the verifiability
  requirement names, a well-known file and a DNS record.
- Go vanity imports: a domain serves a meta tag binding an import path to a
  repository; control of the HTTP response is the proof.
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
