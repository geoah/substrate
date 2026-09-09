---
status: accepted
date: 2026-09-09
decision-makers: George Antoniadis
---

# 0074. The repository name is the login name, and `username` is gone

## Context and Problem Statement

A user had two names. `repositories.username` was the login identifier
(`ada`), unique across the substrate and the key every door started from;
`repositories.id` was the authority (`ada.example.com`), unique and permanent
too, and the repository's name on disk, in the database and on the wire
([0052](0052-the-authority-is-the-repository-id.md)). Registration asked for
both, so the one form had a `username` field and an `authority` field whose
default was built out of it. 0052 left the username as "the login label" and
0046 called it "the login identifier"; nothing else needed it, and two unique
names for one repository is one name too many.

## Considered Options

- Drop `username`: the repository's name IS its authority, and that is what a
  registration asks for and a login presents.
- Keep both columns and hide the username in the console, the status quo with
  a nicer form.
- Keep `username` as the login key and derive the authority from it with no
  choice at registration, which is 0046's default made mandatory.

## Decision Outcome

Chosen: the repository name is the login name. `POST /register` and
`POST /login` take one `repository` field; the credential endpoints
(`/password`, `/totp/enroll`, `/totp`, `/recovery/enroll`) take the same
field. The HTTP layer resolves it once
(`vocabulary.RepositoryAuthority`): a name carrying a dot IS the authority, a
bare label is completed under the host the request reached (`ada` reaching
`substrate.example` is `ada.substrate.example`), so 0046's default survives
without a second input. The engine requires a concrete authority, holds it to
`ValidRepositoryAuthority`, refuses one under `substrate.reamde.dev` and
refuses one another repository owns; every lookup is `repositoryByID`.

`repositories.username` and its unique index are dropped (migration 0024).
The credential record renames `username` to `repository`, which the apply
carries out as ordinary record writes
([0063](0063-a-property-rename-is-ordinary-record-writes.md)). The repository
record keeps `name` holding the authority, deprecated in favour of
`authority`, because dropping a property live records hold is a narrowing the
upgrade refuses. `RepositoryInfo` loses `Name`, the delivery envelope loses
`repository.owner` and keeps `repository.authority`, the manifest drops
`username` in format 3 (formats 2 and 1 still read, their value dropped), and
the CLI context stores one `repository` instead of a `username` and an
`authority`.

The rate limiter keys on the resolved repository rather than a username, so
one repository has one bucket however a caller spells it.

### Consequences

- Good, because a repository has ONE name for a person to remember, in the
  login form, the webhook URL, the directory under the data root, the wire
  and the operator's commands.
- Good, because registration is one name field and one password, not two name
  fields whose relationship the reader has to work out.
- Good, because two unique columns that had to stay in step are one column,
  and the second refusal ("that username is taken") is gone with it.
- Bad, because the login name is now long: `ada.example.com`, not `ada`. The
  bare-label completion keeps the typing short only where the substrate's own
  host is the suffix.
- Bad, because a database written before this does not keep its login names:
  migration 0024 drops the column, and such a user signs in with their
  authority (`<username>.<host>` for a default registration) instead. There is
  no way back.
- Bad, because a function body reading `repository.owner` from a delivery
  envelope now reads nothing; the field is `repository.authority`.
- Neutral: the operator's commands take the authority where they took a
  username (`repository verify ada.example.com`, `user reset
  ada.example.com`).

### Confirmation

`TestRegisterThenLogin` and `TestRegistrationKeepsADottedRepositoryName`
(internal/api) pin the one field and both resolutions;
`TestRepositoryAuthorityGrammar` (internal/vocabulary) pins the resolver;
`TestConcurrentRegistrationsForOneAuthorityKeepTheWinner` and
`TestRegistrationCreatesTheUserAndNothingBefore` (internal/engine) hold the
one-name registration and the credential record's `repository` property;
`TestManifestRoundTrip` (internal/changelogfile) holds format 3 writing no
`username`; `TestEnvelopesCarryTheAuthority` (internal/runner) holds the
envelope.

## More Information

Supersedes the "the username stays the login identifier and the engine's
repository key" paragraph of
[0046](0046-a-repository-owns-one-authority-chosen-at-registration.md) and the
"the username is untouched" paragraph of
[0052](0052-the-authority-is-the-repository-id.md); the rest of both records
stands, registration's grammar, publisher-namespace refusal and uniqueness
check included. Reopen trigger: a repository that must be reachable under a
short handle as well as its authority, which would need a second name and the
uniqueness rules to go with it.
