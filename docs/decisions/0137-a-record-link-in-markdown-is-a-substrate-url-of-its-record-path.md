---
status: accepted
date: 2026-09-28
decision-makers: George Antoniadis
---

# 0137. A record link in Markdown is a `substrate://` URL of its record path

## Context and Problem Statement

The console's Markdown editor ([PR 737](https://github.com/geoah/substrate/pull/737))
lets a writer link a record from inside a `markdown` property, and the link
has to be stored as text in that property. Whatever spelling is chosen ends up
in people's prose, and every future reader of it (the console, an agent, an
index of mentions) has to parse it. Changing it later means rewriting stored
text, so the spelling is chosen once.

## Considered Options

- `[text](substrate://<record path>)`: the record path after a URL scheme,
  so the kind's authority is the URL's host
- `[text](substrate://<repository>/<package>/<kind>/<id>)`: the repository
  first, then the rest of the kind
- `[text](ref:<record path>)`: the record path after a bare `ref:` prefix,
  mirroring the stored `{ref: <kind>/<id>}` of a reference property
- `[text](/data/<record path>)`: the console's route for the record

## Decision Outcome

Chosen: `[text](substrate://<record path>)`, for example
`[Ada Lovelace](substrate://ada.example.com/people/person/ada)`. The target
after the scheme is exactly the string a reference property stores, so one
string names a record in a reference, in a REST path
([0033](0033-the-path-grammar-has-no-separators.md)) and in prose.

The repository-first spelling loses the kind's authority. A provider's records
live in the repository under the publisher's authority
(`providers.substrate.reamde.dev/google/contact`), and without it a package
name is ambiguous between authorities. The repository is also implied: there
is no sharing, so a link always means a record in the repository that holds
the text, and a token already names that repository. `ref:` carries the same
path but is no URL, so a generic parser, a renderer's link sanitizer and a
reader of the raw text all see an unknown prefix. The console route ties stored
prose to one client's URL layout.

The link is prose, not a reference: it is not declared, the engine does not
parse it, and it does not appear among a record's referrers. A reference stays
the only link between records
([0044](0044-a-reference-is-the-only-link-between-records.md)).

### Consequences

- Good, because the target is a valid URL: the authority is a DNS-style name,
  so it parses as the host, and the id alphabet has no `%`
  ([0014](0014-authorities-widen-only-outside-the-id-alphabet.md)), so the
  path never needs escaping.
- Good, because the link text keeps a title every other Markdown reader shows.
- Bad, because a URL whose host is an authority looks like the URL-based kind
  identity the project has not designed, and a reader may take it for a
  fetchable address. It is not one.
- Bad, because the stored link text is the title when the link was written
  and goes stale when the record is renamed; the console shows the live title.
- Bad, because a mention index, if one is built, has to parse Markdown to find
  these links.

### Confirmation

`web/console/src/components/markdown/markdown.test.ts` holds the round trip:
a `substrate://` link reads as a record link and writes back unchanged, and
any other scheme stays a plain link.

## More Information

Revisit when kind identity moves to URLs: that design decides whether the host
of this URL stays the authority.
