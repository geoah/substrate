---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0293. A publisher that moves leaves a pointer the server shows and never follows

## Context and Problem Statement

A publisher's namespace is permanent: other repositories hold records under
it. The 1.0 review (F2 point 4) says a publisher that moves leaves a "moved
to" pointer, and says nothing more. Following a pointer would move kinds
between authorities on another person's word, which is the move a publisher
may not make
([0269](0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md)).

## Considered Options

- No pointer.
- A pointer an installing server follows: upgrades from the new authority,
  records moved to it.
- A pointer an installing server shows and never follows.

## Decision Outcome

Chosen: shown, never followed. An authority document may carry `movedTo:
<authority>`. The server reads it as another authority (anything else is
left out, and the document is still read) and shows it on that publisher's
catalog entries (the publisher's `movedTo`). Nothing
installs from it, nothing upgrades from it, and nothing moves between
authorities: a package installed from the old document keeps its kind names,
and keeps upgrading from the old document while it is served. Moving one's
data to the new publisher is the owner's own act, as installing any package
is.

This is the smallest thing that honors the review's one line. A publisher
that has lost the old domain can leave no pointer, and gets nothing from
this. A document a repository's server serves
([0291](0291-a-repositorys-server-serves-what-it-publishes-and-the-authoritys-record-naming-that-document-is-the-proof.md))
carries no pointer in this build; a hand-hosted one may.

### Consequences

- Good, because an owner learns that a publisher moved without anything
  changing under them.
- Bad, because it helps only a publisher that still controls the old
  document.

### Confirmation

The authority reader's tests of the field, and the catalog test that shows
it on an entry.
