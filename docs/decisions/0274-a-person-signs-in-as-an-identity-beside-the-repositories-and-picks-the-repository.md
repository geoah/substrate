---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
amended-by: 0288
---

# 0274. A person signs in as an identity beside the repositories, and picks the repository

## Context and Problem Statement

[0074](0074-the-repository-name-is-the-login-name.md) made the repository's
name the name its person signs in with, and the sign-in itself a record of
the repository: `core/credential` at id `self`, holding two refs into the
repository's sealed store. So a person was a repository. One person could not
open two repositories, a second person could not be added to one without
rewriting what it stores, a password change wrote the repository's history,
and the operator's reset needed the server stopped because it appended to a
changelog the server writes. Rows 21 to 23 of
[pull request 1313](https://github.com/geoah/substrate/pull/1313) (step 18)
separate the three: the person, the repository, and what joins them.

## Considered Options

- An identity table beside `repositories`: the name, the password hash and
  the second factor, sealed under the identity's own key, and a table of the
  repositories each identity may open. Tokens name the identity.
- Keep the credential record and add a person's name to it. The sign-in stays
  in the repository's history, and one person per repository stays built in.
- Identities as records of one system repository: a history for sign-ins,
  bought with a repository every sign-in depends on and a bootstrap for it.

## Decision Outcome

Chosen: an identity table beside `repositories`, because it holds a person in
any number of repositories with no stored shape to change, keeps the factors
out of every repository's history, and depends on no repository.

- **The tables.** `identities` holds the id (`idn-…`), the name, and the
  factors sealed under a per-identity data key that the host credential key
  wraps, as a repository's is: the same AEAD, the same key, nothing weaker.
  `identity_repositories` holds one row per repository an identity may open:
  the server's index of the person actors (below).
  Both are control-plane tables the repository role never sees.
- **The name.** A sign-in name keeps a repository name's DNS-style grammar, so
  another server could one day vouch for it, and a bare label is completed
  under the host the request reached. It is unique on the server and no
  longer the repository's name, though registration gives both the same one
  unless it names the repository.
- **The wire.** `POST /register` and `POST /login` take `identity`, and
  `repository` where the request names one; the credential changes and the
  sign out everywhere take `identity`; `POST /repository/delete` takes both.
  A sign-in that names no repository opens the person's one repository, and a
  person in several is answered `409 conflict` with reason
  `repository_required` and the list, with no code spent, so the same request
  naming one signs in. A body without `identity` is answered `400` naming it.
  Wrong password, wrong code and unknown name stay one answer.
  `GET /identity` reads the person a token acts for. The `auth` feature is at
  revision 3.
- **Tokens.** A token record names the identity it acts for; a mint by a
  token, an app's approval included, inherits it, and stands on that token
  still authenticating. A token authenticates while the identity it names
  may open its repository. Sign out everywhere ends the person's tokens in
  every repository they open, with the tokens there that name nobody. The
  limits per person count by identity.
- **A proof holds to its act.** A request that proves the factors acts
  under the identity's row, the lock every change of them takes, and checks
  there that they are the ones it proved, so a sign-in, a credential change,
  a sign out everywhere or a delete proved before a change acts on nothing.
- **The archive.** Each repository's archive carries its people: every
  manifest names, under `identities`, the row of each person the index names
  for it, as stored. Of a person, the archive holds the name, the sealed
  password and the sealed second factor of everyone in each repository,
  useless without the host key. An export carries the person who took it and
  nobody else, because the other people's sign-ins are theirs. A restore
  inserts, in the transaction that creates the repository's row, each person
  the database does not hold; a row the database held before the restore
  began stays as it is, and among the copies of one person that one start's
  restores bring, the one whose factors changed last wins. A person whose
  copy does not open under the host's key follows the repository's own rule
  (the restore is refused unless the operator listed it, and then it leaves
  them out), and a person whose name another person here holds is left out
  and logged. `identities` joined manifest format 3 without a new number,
  because no release had carried format 3; a build of `main` from before it
  refuses a manifest with the key, which matters only when going back
  between two builds of `main`. A manifest without it, which is every
  archive written before, still holds the sign-in record of a release before
  identities, and the boot makes the person from it, as it does for a
  repository upgraded in place.
- **Deleting.** Deleting a repository removes its rows of
  `identity_repositories`, and the deleting person's identity with its last
  one; the name is free again.
- **Repositories from before.** The boot gives every repository holding a
  credential record and no identity an identity named after it (a name taken
  since, only by a restore, becomes `<repository id>.<name>`, logged), with
  the hash and the seed opened and sealed again unchanged, in one
  control-plane transaction. A repository migration purges the record and its
  sealed rows at the first open, and an open step after the shipped upgrade
  names the person of every token that names none.
- **The operator.** `substratectl user reset <name>` writes the identity row
  alone, so the server may keep running; a repository whose person actor
  names an identity no identity here holds, one a restore did not bring, is
  named instead and gets that person back, under the same id, so their
  tokens authenticate again, whoever else signs in to it; with several
  such, `--identity` picks one by the name the actor carries. Two resets of
  one repository are one at a time.
- **A person is an actor.** A person is in a repository while a live
  `core/actor` record of type `person` there names their identity
  (`identity`); the history and an export carry it. The repository's first
  person is the actor keyed `person`, another person's is keyed by their
  identity, and a person's writes link their own. `identity_repositories`
  is the server's index of those actors for the identities it holds, what a
  sign-in and a token are checked against. Two invariants hold it. It is
  never ahead of the actors: while the server admits anyone it names nobody
  the live person actors do not name, every derivation is exact to the
  empty set for a repository whose history has been given identities, and
  nothing writes again a person's actor that is disabled or tombstoned, nor
  starts work for that person, and what they left running is settled for
  nobody; a repository from before identities, told apart by what the
  server recorded in it (an actor kind that declares no identity, a sign-in
  record, or a line of one), keeps its person until its first open names
  them on the actor. It is behind them only while the database refuses the
  write: the index row a request writes after a person's actor does not end
  with the request, and is tried again until it lands or the process stops,
  when the boot's derivation finishes it. Wherever actors arrive another
  way (an import, a restore, a conversion, a rebuild) the index is derived
  again from them before the repository is offered to anyone, at every boot
  and at the end of a rebuild, taking out under the token cache's fence the
  people no actor names. An open repairs nothing. A reset reads the actors
  under the repository's lock: an actor naming an identity the server holds
  is the repository's person, and the reset makes no other; a repository
  whose history took its person out gets a new person with an actor of
  their own, and only from the repository's writer.

### Consequences

- Good, because a second person, or a person in a second repository, is one
  row, and no stored shape changes for it.
- Good, because a password change and a reset write no repository's history
  and need no stopped server.
- Good, because the factors' strength is unchanged: the same argon2id hash
  and AEAD under the same host key, with a key of the person's own.
- Bad, because a person comes back from an archive only where it carries
  them: a restore of an archive written before it carried people, of an
  export someone else took, or of a person left out needs `user reset` to
  sign in, which gives the person back under the identity their actor names.
- Bad, because the archive holds every person's sealed factors beside the
  repository's data, so an archive kept beside the host key is every
  person's sign-in too, as it is every secret.
- Bad, because a client from before this cannot sign in to a server after it,
  and the reverse: the door refuses the other's body. Servers move first, and
  a client must read the `auth` revision to speak to both.
- Bad, because the history no longer records that a password changed; only
  the identity row's `updated_at` does.

### Confirmation

`TestASignInNamesThePersonAndReachesTheirRepository`,
`TestAFactorChangeWritesNoRepositorysHistory` and
`TestOnePersonInTwoRepositories` (internal/testenv) hold the wire, the
history and the shape over HTTP on the engine; `TestKeptDirectories` signs in
to the kept v0.123.1 directory with its password, and
`TestABootGivesARepositoryFromBeforeIdentitiesItsPerson` (internal/engine)
does the same for a database upgraded in place;
`TestNoTableOrPathIsKeyedByTheName` allows the name in `identities.name`
alone. `TestARestoredPersonSignsInWithTheirLatestCopy`,
`TestARestoreLeavesAPersonTheDatabaseHeld`,
`TestAnExportCarriesThePersonWhoTookIt`,
`TestARestoredPersonWhoseNameIsTakenIsLeftOut` and
`TestAChangeOfAPersonsFactorsIsArchived` (internal/engine) hold the
archive's people.

## More Information

Supersedes [0074](0074-the-repository-name-is-the-login-name.md): the
repository's name stays its authority and stops being the sign-in. Amends
[0236](0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md):
the person's name is freed with their last repository. Reopen trigger: a
person signing in with an identity another server vouches for.
