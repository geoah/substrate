---
status: proposed
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0203. An app keeps its own package under a declare scope

## Context and Problem Statement

Substrate Workouts logs strength sessions down to the set, in five kinds of
its own (`exercise`, `setup`, `routine`, `session`, `set`) that no catalogue
ships and that should not ship with every server: they are the app's, and
change with its releases. An app's token cannot declare anything
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
and the approval's `package` detail installs only what the server's catalog
ships ([0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)),
so an app that brings its own kinds had no way in short of the owner's
password, which 0194 set out to end. The app also has to keep its package
current across its own releases and say so when a repository holds a newer
one.

## Considered Options

- Ship the kinds as a first-party catalogue, installed by the `package`
  detail.
- Carry the manifests in the authorization request, set up on approval.
- The `package` detail names a URL the server fetches the manifests from.
- A scope, `declare:~/<package>`, that lets the app's token apply the
  declarations of one package of the repository's own authority, which the
  app then installs and upgrades itself.

## Decision Outcome

Chosen: the `declare:~/<package>` scope, because it is the smallest change
that lets an app own its vocabulary while the owner approves exactly what it
may touch. A first-party catalogue was rejected by the owner: the server
would ship and version an app's kinds. The request cannot carry the
manifests (they are about 90 KB; a request's details are bounded at 2 KB),
and a fetched URL would make the server dial out for a loopback client,
which has no document.

**The package lives under the repository's own authority**, `~/training`,
which lands as `<authority>/training`. That is where a repository's own
vocabulary lives ([0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)),
and where an imported sample is rehomed
([0048](0048-providers-are-published-samples-are-copied.md)): the
owner owns it, edits it and sees it as theirs. No authority the app does not
own is claimed, nothing reads as published by the server, and the scopes the
app already writes with `~` (`write:~/training/*`) name the same package.

**What the scope admits, and nothing else:**

- `POST /api/v1/vocabulary/apply` with documents that are each a `package`,
  `bundle` or `kind` declaration of the named package. A function, trigger,
  agent, mapping or trait, or any other package, refuses the batch with 403.
  So 0202's objection to an installing scope, that a token which installs
  ships code whenever it likes, does not reach it: no code can be declared.
- No lossy change. The confirmation a lossy apply needs ([0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)) is refused to
  an app's token, so an upgrade may only widen; a narrowing waits for the
  owner.
- The package's own `package` and `bundle` rows, readable, so the app knows
  the version the repository holds.
- The engine's own rules still hold: a `builtin` or `published` package is
  refused (`authorizeDeclarationWrite`), and the publisher's authority stays
  closed (`authorizeNewPackage`).

A request may ask only for `declare:~/<package>`: a package named by another
authority, a glob or a kind is refused before the approval page.

**The app manages the package.** At sign-in and at each launch it reads the
package row: missing, it applies its bundled manifests; older, it applies
them again (the engine lands a changed declaration at the next version); the
same, nothing; newer than it knows, it writes nothing more and says to update
the app. The package requires `substrate.reamde.dev/health`, which the
approval's `package` detail installs first.

### Consequences

- Good, because an app's kinds stay the app's: it ships, versions and
  documents them, and a server ships nothing for them.
- Good, because the approval page says what the scope does ("Install and
  update the kinds of your Training package"), and the token can do nothing
  else with the vocabulary.
- Bad, because kind names differ per repository (`ada.example.com/training/set`),
  so two repositories' training records are not one name; the app rehomes
  `~` at the server's edge, and an importer or agent works from the
  repository's own name.
- Bad, because an owner who edits the package by hand moves its version past
  the app's, which the app then reads as a newer version and stops writing.
- Bad, because `scopes_supported` lists `declare:~/<package>`, a shape rather
  than a scope, since no wider declare scope exists.

### Confirmation

`internal/scope` tests the grammar (`TestADeclareScopeKeepsOnePackageOfTheOwnersOwn`);
`internal/api` tests what the scoped apply admits and refuses
(`TestAnAppDeclaresOnlyThePackageItsScopeNames`). WorkoutsKit's tests hold
the app's side, and its live test runs the whole path against a server:
approval, health installed, the package declared, a session written.

## More Information

The app and the package's long form are in
[Substrate Workouts](../../apps/apple/SubstrateWorkouts/README.md) and
[its package](../../apps/apple/SubstrateWorkouts/Package/README.md); the
scope is in [apps](../apps.md#keep-a-package-of-your-own). Revisit when a
second app brings its own kinds, or when kind identity moves to URLs, which
would let an app publish under an authority of its own.
