---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0086. The head of a repeated source is spelled `[]`, in a display template and in `merge: first`

## Context and Problem Statement

A provider that mirrors an API array verbatim holds a repetition its subject
does not want. A Google contact's names are
`names[]{displayName, givenName, metadata{primary}}` and a person has one name,
so on Mneme v6's seeded repository 150 of 151 contacts' persons were nameless
and 2,416 persons minted from a bare address showed a record id where a title
belongs. Two doors refused the same shape. A `displayTemplate` token was
`{prop}` or `{ref.prop}` only, so `{names[].displayName}` did not parse and the
kind titled itself `people/c123`. A map rule had no way to take one item out of
a repeated source: `merge: union` needs a REPEATED target, without `merge` the
two ends must agree on repetition, and `names[0]` is not in the path grammar —
so the owner's best source of human names could PROBE a person by name and
never SET one (geoah/mneme-v6 `docs/upstream.md` ask J).

## Considered Options

- A predicate on the source entry (`where: {metadata.primary: true}`), which is
  the semantically correct answer and a much larger change: a second expression
  language inside a declaration that deliberately has none
- A subscript (`names[0].displayName`), which promises an order the provider's
  array does not have
- The provider computes a single-valued scalar beside the array
  (`primaryName`), which every provider must then anticipate for every consumer
- `[]` as the HEAD of the list, in both doors: a token alternative and a third
  `merge:` mode

## Decision Outcome

Chosen: the fourth. `[]` already means "walk the repetition" in a mapping's
path grammar (`ParsePath`), so a reader who knows `emails[].value` in a `map:`
rule knows `{names[].displayName}` in a template and `merge: first` beside the
same path; nothing new is spelled, and no expression language is admitted.

**A display template alternative may be a list path.** `{names[].displayName}`
is the first entry's field of a repeated object, `{assignees[].login}` the
first referent's property of a repeated reference, and `{emails[]}` the first
value of a repeated scalar. The head is a suffix, never a subscript. An empty
list renders "" and the token's next alternative gets its turn, which is the
whole point: `{names[].displayName|resourceName}` titles every row a provider
sends. THE FIRST ENTRY THAT RENDERS SOMETHING is taken, not entry zero — an
entry whose field is absent would otherwise title a record with nothing while
the next entry held the name.

**A map rule may say `merge: first`**: the head of a repeated source onto a
single-valued target. It is `union`'s question from the other side, and the
cardinality check reads that way — `union` needs a repeated target, `first`
needs a single one, and a repeated source under neither still needs a repeated
target. An EMPTY source contributes nothing rather than an empty value, so a
contact whose `names[]` is empty leaves the person's name to whatever else
offers one instead of clearing it. The mode is applied per RULE, in
`contributionOf`, and not per property: it is a statement about the source's
repetition, and the selection across sources stays the ordinary
latest-write-wins.

The type checks are the loader's, on the manifest that caused them: a `[]`
token whose head is not repeated is refused naming the plain spelling, and
`merge: first` onto a repeated target is refused naming `union`.

### Consequences

- Good, because a provider bundle can keep its API's shape verbatim and a
  consumer can still take the one value it wants — names, phones, addresses,
  titles, every array an API sends.
- Good, because both doors learn one spelling, and it is the spelling the path
  grammar already had.
- Bad, because `first` is positional, and position is not primacy: Google marks
  the primary entry with `metadata.primary` and this rule cannot read it. Where
  the provider sorts its array that is right often enough; where it does not,
  the answer is still a derived scalar or, one day, a predicate.
- Bad, because `merge` is a dialect enum and a new value is an upgrade of every
  binary that reads the closure (record 0020): `core/recordmapping` goes to
  version 11, and a repository whose server is older refuses a mapping that
  names `first`.
- Bad, because `{a.b}` over a REPEATED object still loads and still renders
  nothing. Refusing it would be the tidier grammar and would park every stored
  closure that carries one, so it stays legal and the docs say which spelling
  reads a list.

### Confirmation

`TestTemplates` and `TestTemplateTokensValidate` (`internal/vocabulary`) hold
the grammar and the refusals; `TestMappingRules` holds `merge: first`'s
cardinality, both ways. `TestFirstOfAListTitlesTheRecord` and
`TestMergeFirstSetsASingleValuedTarget` (`internal/engine`) hold the rendering,
the fall-through and the empty source that does not clear.

## More Information

geoah/mneme-v6 `docs/upstream.md` ask J, and ticket T-026 there. Reopen for a
predicate (`where:` on a source entry) when a provider that marks its primary
entry matters more than the grammar staying this small.
