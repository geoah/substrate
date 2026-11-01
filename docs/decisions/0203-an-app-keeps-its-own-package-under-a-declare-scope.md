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
  A bundle document may carry only its closure listing (`authority`,
  `package`, `description`, `requires`, `requiresAtLeast`, `installs`):
  `modules` is source every function of the package imports and `oauth2`
  makes the package a provider at endpoints the app would choose, so either
  refuses the batch, as does `inputs`, a kind's `indices` and a property's
  `embed`, which spend the owner's database and provider. So 0202's
  objection to an installing scope, that a token which installs ships code
  whenever it likes, does not reach it: no code can be declared.
- Only additions. The confirmation a lossy apply needs ([0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)) is refused to
  an app's token, and the lossy guard alone protects only values live
  records hold, so the scoped apply also refuses, with 409, a batch that
  would take away anything the repository holds in the package: a kind
  document leaving out a stored property, field, enum value, state,
  transition or trait, and a bundle (which replaces its package whole) that
  does not list every declaration the stored bundle lists. The owner's
  additions to the package therefore survive the app's upgrades, and any
  removal, the app's own included, waits for the owner.
- The package's own `package`, `bundle` and `kind` rows, readable, so the
  app knows the version the repository holds and the version of each kind.
- The engine's own rules still hold: a `builtin` or `published` package is
  refused (`authorizeDeclarationWrite`), and the publisher's authority stays
  closed (`authorizeNewPackage`).

A request may ask only for `declare:~/<package>`: a package named by another
authority, a glob or a kind is refused before the approval page.

**The app manages the package.** At sign-in and at each launch it reads the
package row: missing, it applies its bundled manifests; older, it applies
them again (the engine lands a changed declaration at the next version); the
same, nothing; newer than it knows, it writes nothing more and says to update
the app. A kind edit moves the kind's version and not the package's, so the
package version cannot tell an owner's edit from the app's: the app records
each kind's version after every apply it makes, and before an upgrade it
compares them. A kind whose version moved, or one it never installed, is the
owner's change, and the app stops and asks before applying over it. The package requires `substrate.reamde.dev/health`, which the
approval's `package` detail installs first.

### Consequences

- Good, because an app's kinds stay the app's: it ships, versions and
  documents them, and a server ships nothing for them.
- Good, because the approval page says what the scope does ("Install your
  Training package and add to it as the app changes; it can't remove a kind,
  property or value the package holds, yours included, or add code"), and
  the token can do nothing else with the vocabulary.
- Bad, because kind names differ per repository (`ada.example.com/training/set`),
  so two repositories' training records are not one name; the app rehomes
  `~` at the server's edge, and an importer or agent works from the
  repository's own name.
- Bad, because an owner who edits a kind of the package by hand stops the
  app's next upgrade until they choose to let it apply over the edit; an
  owner who adds a kind or a property keeps it, and an upgrade that would
  remove it is refused until the owner removes it or applies it themselves.
- Bad, because the guard reads the stored package before the engine applies
  the batch, outside its transaction, and the apply takes no expected
  version: an owner's edit landing between the read and the apply is not
  seen. Closing it needs an expected-version precondition on
  `/vocabulary/apply`.
- Bad, because `scopes_supported` lists `declare:~/<package>`, a shape rather
  than a scope, since no wider declare scope exists.

### Confirmation

`internal/scope` tests the grammar (`TestADeclareScopeKeepsOnePackageOfTheOwnersOwn`);
`internal/api` tests what the scoped apply admits and refuses
(`TestAnAppDeclaresOnlyThePackageItsScopeNames`,
`TestADeclaredBundleListsItsKindsAndNothingElse`,
`TestAnAppsApplyNeverRemovesWhatThePackageHolds`). WorkoutsKit's tests hold
the app's side, and its live test runs the whole path against a server:
approval, health installed, the package declared, a session written.

## More Information

The app and the package's long form are in
[Substrate Workouts](../../apps/apple/SubstrateWorkouts/README.md) and
[its package](../../apps/apple/SubstrateWorkouts/Package/README.md); the
scope is in [apps](../apps.md#keep-a-package-of-your-own). Revisit when a
second app brings its own kinds, or when kind identity moves to URLs, which
would let an app publish under an authority of its own.
