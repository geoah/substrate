---
status: accepted
date: 2026-11-08
decision-makers: George Antoniadis
---

# 0246. A declared property a derived-only token shadows is spelled with a leading dot

## Context and Problem Statement

A `displayTemplate` token names a declared property, a reference path, a list
head ([0086](0086-the-head-of-a-repeated-source-is-spelled-with-brackets.md)),
or a derived token. `{localName}` and `{id}` yield to a declared property of
their name. `{snippet}` never has: it is always the engine's excerpt of the
record's body, so a kind that declares a `snippet` property of its own had no
token that read it. Google's `gmailmessage` declares Gmail's `snippet`
verbatim, and a message without a subject that was only read through a thread
listing has no body, so it rendered untitled although Gmail sent a preview
(#1286).

## Considered Options

- Let `{snippet}` yield to a declared property, as `{localName}` and `{id}` do
- Rename Gmail's property (`preview`, with `renamedFrom: snippet`)
- Leave such messages untitled
- A leading dot, `{.snippet}`, for the record's own declared property, accepted
  only where the bare name is a derived-only token

## Decision Outcome

Chosen: the leading dot. `{.name}` reads the record's own declared property
and renders as a bare property does; it is accepted only where the bare
spelling is a derived token that never yields to a declaration, which today is
`snippet` alone. `{.label}` and the like are refused naming the bare spelling,
the property must be declared, and a sensitive one is refused as the bare form
is. `gmailmessage` titles itself `{subject|.snippet|snippet}`: the subject,
else Gmail's preview, else the body excerpt.

Making `{snippet}` yield would silently change what every stored kind that
declares `snippet` already renders, and would take the body excerpt away from
those that want both. A rename is an ordinary write to every stored Gmail
message at the upgrade
([0063](0063-a-property-rename-is-ordinary-record-writes.md)) and moves the
property off the name Gmail's API gives it. Leaving the messages untitled is
the bug.

### Consequences

- Good, because no template already shipped changes meaning and no stored row
  is written.
- Good, because a derived-only token added later has its declared twin spelled
  the same way, without a new rule.
- Bad, because a binary older than this one cannot parse `{.snippet}`, so
  rolling back past it parks every package whose template uses it: the Google
  provider from its version 48 on.
- Bad, because the template grammar is part of a closed dialect
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)): once a shipped
  package uses `{.name}`, removing it means superseding this record.
- Bad, because one name now has two spellings with different meanings, and a
  reader has to know that `{snippet}` is the excerpt and `{.snippet}` the
  property.

### Confirmation

`TestADeclaredSnippetIsSpelledWithADot` in `internal/vocabulary/dialect_test.go`
holds the parse, the refusals and the render, and
`TestGoogleGmailRowsAreTitledByWhatTheMailSays` in
`internal/providertest/google_gmail_threads_test.go` holds the Gmail titles.

## More Information

Revisit if a second derived token becomes derived-only, or if `{snippet}` is
ever made to yield to a declared property, which would leave the dot with
nothing to say.
