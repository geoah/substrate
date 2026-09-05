---
status: accepted
date: 2026-09-05
decision-makers: George Antoniadis
---

# 0052. The authority is the repository id

## Context and Problem Statement

A repository had two names. `repositories.id` was a random 12-character
string minted at registration: the row-level-security scope value on every
table, the additional data the DEK wrap is bound to
([0023](0023-a-sealed-payload-is-bound-to-its-address.md)), the name of the
directory under the data root
([0051](0051-a-repository-directory-is-the-backup-unit.md)) and
`RepositoryInfo.id` on the wire. `repositories.authority` was the DNS-style
name the user chose at registration
([0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)),
unique and permanent, the home of every kind the user declares. Both were
unique, both were permanent, and nothing could change one without the other.
An operator browsing the data root met `repositories/k3j9x2m41pfq/` and had
to open the manifest to learn whose it was.

## Considered Options

- Keep the random id and the authority as two columns: the status quo of
  0046 and 0051.
- Make the authority the repository id: `repositories.id` holds the
  authority and the random id is removed.
- Derive the id from the repository's signing key, as
  [#341](https://github.com/geoah/substrate/issues/341) and
  [#285](https://github.com/geoah/substrate/issues/285) proposed. Not
  possible since [#355](https://github.com/geoah/substrate/pull/355) removed
  the signing key ([0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)).

## Decision Outcome

Chosen: the authority is the repository id. `repositories.id` holds the
authority (`ada.example.com`), the row-level-security scope value is the
authority, the DEK wrap's additional data is `dek\x00<authority>`, the
directory is `<root>/repositories/<authority>/` with the fs blob path under
it, `RepositoryInfo.id` on the wire is the authority, and nothing mints a
random id any more. The manifest `repository.json` drops its `id` field and
carries `authority`. One name for the repository on disk, in the database and
on the wire; the directory a person browses is `repositories/ada.example.com/`;
two columns that had to stay equal were one column too many.

There is no data migration for rows written with a random id. A database
whose `repositories.id` is not an authority is refused at boot with the
instruction to wipe the database and boot again. The repository directories
import on that boot: a directory still named by the old id is renamed to its
authority and its DEK is re-wrapped under the new additional data. The tree
assumes fresh repositories before v1, as 0046 already did.

The username is untouched. It stays the login label (`/login`,
`repository.owner` in a delivery envelope) and is renameable in principle;
the authority is not. Of 0046, only the "two distinct columns" paragraph is
replaced: the registration input, the `<username>.<request host>` default,
the grammar, the publisher-namespace refusal and the uniqueness check all
stand, and registration is unchanged for a client. Of 0051, only the "named
by the id" clause is replaced: the directory's contents, the manifest's DEK,
the boot cases and the backup procedure all stand.

### Consequences

- Good, because the repository has one identity everywhere: the directory,
  the scope, the wrap's additional data, the manifest and the wire carry the
  same string, and `repositories/ada.example.com/` needs no `grep` to find.
- Good, because one unique column is gone and with it the check that two
  columns still agree.
- Bad, because the authority is now truly permanent: a rename would mean
  rewriting every scoped row, re-wrapping the DEK and moving the directory,
  which forecloses the cheap rename #341 imagined.
- Bad, because a typo at registration is forever. Nothing corrects
  `ada.exmaple.com` short of a new repository.
- Bad, because the identity is a user-chosen string rather than something the
  server minted, so it appears in URLs, logs and directory listings as the
  user typed it, and a scope value is now guessable from a public name.
- Bad, because a database from before this record does not boot: the
  operator wipes it, and the directories under the data root are what comes
  back.

### Confirmation

`TestRegistrationUsesTheAuthorityAsTheId` (internal/engine) asserts the
control-plane row, the directory name and `RepositoryInfo.ID` all carry the
authority. `TestBootRefusesARowWhoseIdIsNotItsAuthority` holds the refusal
and its message. `TestBootImportsAnOldIdNamedDirectory` holds the rename and
the re-wrap on import.

## More Information

Supersedes the "two distinct columns" paragraph of 0046 and the "named by the
id" clause of 0051; the rest of both records stands as written above.
[The plan](../plans/filesystem-changelog.md) has the directory layout.
Reopen trigger: a repository that must change its authority, or domain
verification binding the authority to something the server can check. Either
would need the rename machinery this record declines to build.
