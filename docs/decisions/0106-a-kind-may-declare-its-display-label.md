---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0106. A kind may declare its display label, singular and plural

## Context and Problem Statement

A client names a collection from the kind's name, split and pluralized by a
word list of its own. Some shipped kinds read wrong however the name is split:
Slack's `conversation` is what a person calls Channels, Google's
`calendarseries` is Repeating events, and every `*sync` state kind is Sync
progress ([#675](https://github.com/geoah/substrate/issues/675)). A fix in one
client's word list leaves every other client wrong. Decision
[0033](0033-the-path-grammar-has-no-separators.md) removed plurals from
routing and retired the `plural` declaration key; the working reading since
was that plural forms are a client's business and never in the API. The owner
ruled on #675 that a kind declares a display label.

## Considered Options

- No key: the word list stays in each client.
- A singular `label:` string, with plurals left to clients.
- A `label:` block carrying both forms, `singular` and `plural`.

## Decision Outcome

Chosen: the `label:` block with both forms, because the collection heading is
what reads wrong and it is a plural. A singular alone would send every client
back to its own pluralizer, and a mass noun such as "Sync progress" has no
regular plural to derive.

```yaml
names:
  singular: conversation
label:
  singular: Channel
  plural: Channels
```

The block is optional. When present, both keys are required, each a trimmed
single-line caption of at most 80 characters, the same bound as a property's
`displayName`. The loader holds the key set closed and refuses anything else.
The vocabulary read (`KindInfo`) carries it as `label: {singular, plural}`,
absent when the kind declares none.

This amends 0033 in one sense only: a plural FORM is now declarable, as
display text. 0033's rule stands: the collection segment is the kind's name,
the retired `plural` key stays refused at the top of a declaration and in
`names`, and no route, filter, reference, grant or CEL binding reads a label.

### Consequences

- Good: every client shows the same words for a kind, and the console's word
  list shrinks to the fallback for kinds that declare no label.
- Good: a provider author, who knows what the service calls a thing, writes
  the label beside the declaration.
- Bad: one more dialect key. A binary older than this one refuses a document
  that declares it (decision
  [0020](0020-dialect-keys-are-reserved-not-tolerated.md)). After a rollback,
  the older binary reads stored rows through its own key set, drops `label`,
  and serves those kinds without one.
- Bad: labels are English only; nothing here localizes them.

### Confirmation

`TestKindLabel` and `TestKindLabelRefusals` (`internal/vocabulary/label_test.go`)
hold the grammar. `TestTypeInfoCarriesTheDeclaredLabel` holds the read.
`TestShippedKindsDeclareTheirDisplayLabels` (`kinds/kinds_test.go`) holds the
labels on the shipped kinds #675 lists. The wire golden pins `KindInfo.label`
and `KindLabel`.

## More Information

Revisit if labels need translating: a locale map would replace the two
strings.
