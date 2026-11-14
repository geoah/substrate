---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0289. A repository publishes a package by moving it out of the local namespace to an authority it proves

## Context and Problem Statement

"Authority" meant two opposite things: a publisher's name, which other
repositories hold records under and which must never change, and a
repository's name, which a rename now moves
([0287](0287-a-repositorys-name-is-an-alias-and-a-former-name-holds-for-90-days.md)).
A repository's own kinds live in `~`
([0259](0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)), so
nothing another server can install has a name of its own. The 1.0 review (F2,
rows 17 and 19) asks for three sorts of namespace and for publishing as a
deliberate step.

## Considered Options

- Publish under the repository's name, so a package keeps its `~` spelling
  and the name is the publisher.
- Copy the package out to a proven name and leave the original in `~`.
- Move the package out of `~` to a proven name, once, for good.

## Decision Outcome

Chosen: move it. There are three sorts of namespace, each with its own rule:
the project's (`substrate.reamde.dev`, decision 0292), a publisher's (a domain
whose DNS its owner controls, permanent, decision 0291 for the proof), and
the local one (`~`, the repository's, with no global name). Publishing
`~/recipes` as `ada.example.com/recipes` is one vocabulary batch in one
transaction, as ordinary writes:

- The package's declarations land under the authority, with every name in
  them that pointed at the package (an id, the declaring authority, a name
  at a name key, a stored grant, a kind spelled in a function's source or an
  agent's prompt) moved with it. Another package of the repository's own
  that names it follows in the same batch.
- Its hands move with it: the package, its functions and its agents write
  as `bundle:<authority>:<package>` and the like from then on, and the actor
  records of their hands under `~` are disabled, as an uninstall disables
  them ([0270](0270-every-hand-that-writes-is-an-actor-record.md)), so the
  writes before still link them.
- Every live record of its kinds is carried to the kind of the same name
  under the authority by the kind move of
  [0078](0078-a-kind-move-is-ordinary-record-writes.md) (same id, properties,
  managers and annotations, every reference at it repointed, the old row
  tombstoned), without a `movedFrom` key, so no published closure ever holds
  one ([0269](0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md)).
- A trigger that calls one of its functions or agents, and an app's token
  scope over it, follow.
- The package in `~` is removed whole, and the first release is written in
  the same transaction.

A crash leaves the repository as it was or published, never between. History
keeps the package's old lines under `~`, and the move after them.

This is not the rename 0269 refuses: before the publish no other repository
holds a record of these kinds. A package of another authority, the project's
name and a name under it, a package that names another package of the
repository's own, and a package holding its own settings or secrets are
refused, with nothing changed.

A name a package of the repository's own is published under is a publisher's
namespace there, not a spelling of `~`: the full-name fold of 0259 and 0287
skips it. So a repository named `ada.example.com` that publishes under its
own name reads `ada.example.com/recipes/recipe` as the published kind, and
writes its other packages as `~`.

This amends [0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)
(a repository's name is not its publisher's name),
[0210](0210-a-token-declares-new-packages-only-under-its-repositorys-authority.md)
(the publish is the one door that creates a package outside `~` for a
repository's hand), and 0259 (the fold's exception).

### Consequences

- Good, because a rename never touches a published namespace, and a
  published package is a name other servers can hold records under.
- Good, because the move reuses the shipped upgrade's move: one carrier of
  rows between kinds.
- Bad, because the repository's full name stops folding to `~` once it
  publishes under that name; a client that spelled its other kinds by the
  full name must say `~`, which every reply already does.
- Bad, because a package that names another local package must wait for it,
  and settings and secrets of the package block the publish in this build.
- Bad, because publishing is permanent: there is no unpublish.

### Confirmation

`TestPublishingMovesAPackageOutOfTheLocalNamespace` (engine) and
`TestARepositoryPublishesAPackageAndAnotherServerInstallsIt` (two servers,
over HTTP), the vocabulary's `PublishDocuments` tests.
