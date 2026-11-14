---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0261. A repository is keyed by a minted id, and its name keeps every other job

## Context and Problem Statement

[0052](0052-the-authority-is-the-repository-id.md) made a repository's name,
its authority, the key of its storage: `repositories.id`, the `repository`
column of every table and the row security that reads it, the data key's
binding, the directory under the data root and the webhook address. It
priced that honestly: the name became permanent. A name has to be able to
change: a typo at registration, a person who started on `localhost` and later
has a domain, a person who moves hosts. While the name is the key, renaming
means rewriting every row and moving every file, and every address an outside
service holds breaks. The plan in
[pull request 1313](https://github.com/geoah/substrate/pull/1313) (F2, point 1,
and row 14) separates the two; this record is that step, the key alone.

## Considered Options

- Keep the name as the key, and rewrite every row and file at a rename.
- Freeze the name a repository had when ids arrived as its key, and mint ids
  only for repositories created after.
- Mint an id for every repository, and convert each existing one once.

## Decision Outcome

Chosen: mint an id for every repository and convert each existing one once,
because only then is nothing keyed by a name, and a rename (the plan's step
10) touches no storage.

What this sets:

- **The id** is minted at creation, `rnd-` and 128 random bits in the minted
  record id's alphabet (`vocabulary.NewRepositoryID`). It is never shown as a
  name and never changes. Its prefix names how it was made, so a later id
  that can be proven from outside takes another prefix. It holds no dot, so
  an id is never an authority, and a path segment or an address tells the two
  apart by that alone.
- **The id keys storage**: `repositories.id` and the `repository` column of
  every table, so the row security and every scope; the directory
  `repositories/<id>/` and the manifest, now format 2, which carries `id`
  beside `authority`; the data key's wrap binding `dek\x00<id>`; the binding
  of a spooled webhook request; the writer lease; and the webhook address the
  server gives out, `/webhooks/<id>/<trigger>`.
- **The name keeps every other job**: what a person registers and signs in
  with, the namespace of their own kinds, the actor names in history, DNS
  domain verification, and the `repository` a token's answer names. Metrics
  name a repository by its name. The name is unique on the substrate as
  before, and still permanent until a rename lands.
- **Every door that names a repository takes either**: a dataset, every
  operator command, `OpenScopedDB` and the webhook route, so an outside
  service's address by name keeps reaching the same repository.
- **The repository's own record** (`core/repository`) is under the id;
  repository migration 6 moves an existing one as ordinary writes.
- **Existing repositories are converted at the boot**, before the boot check:
  one transaction per repository moves every row from the name to a new id
  and rewraps its data key, then the spooled requests are sealed again under
  the id, the manifest is rewritten and the directory renamed. A directory a
  release before ids wrote, restored with no row, takes a new id the same way
  before its import. A crash between the halves is finished by the next boot.
  A process that does not run the boot (a read-only one) refuses a repository
  still keyed by its name.

Rewriting every row and file at a rename leaves the cost in the operation
that has to be cheap and safe, and breaks every outside address each time.
Freezing a legacy key keeps a name in the key of the oldest repositories for
good, which is the thing being removed, and leaves two shapes of key to read.

The conversion of the rows is not a SQL migration, because it writes the rows
of the fold tables (`repository rebuild` writes them under the scope, which is
the id), and it is not a step of the changelog's conversion chain
([0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)),
because no history line changes: a line names kinds and actors, which stay the
name's. SQL migration 0020 drops the constraint that held the id equal to the
name, and changes the schema alone.

### Consequences

- Good, because a rename can change the name and nothing else.
- Good, because an address by id survives a rename, and the address by name
  keeps working for the services that hold it.
- Bad, because the conversion rewrites every row of every repository at the
  first boot of this release, while the boot waits: about 600,000 rows in
  6 seconds on a laptop's Postgres. It is timed at 100 GB before 1.0.
- Bad, because the directory under the data root is named by an id again, as
  before 0052, and a person browsing it reads the manifest or
  `substratectl repository list` to learn whose it is.
- Bad, because an older server cannot open a converted repository: the way
  back is the copy taken before the upgrade.

### Confirmation

`TestNoTableOrPathIsKeyedByTheName` (`internal/testenv`) registers a
repository and fails on any unique key column or data root path that holds
its name. `TestARepositoryKeyedByItsNameTakesAnIDAtTheBoot` and
`TestAKeyingCutShortIsFinishedByTheNextBoot` (`internal/engine`) convert a
repository in the shape a release before ids left, whole and cut short.
`TestKeptDirectories` imports the directory v0.123.1 wrote and compares every
record.

## More Information

Supersedes [0052](0052-the-authority-is-the-repository-id.md).
[0074](0074-the-repository-name-is-the-login-name.md) stands until the name
becomes an alias and sign-in names a person (the plan's steps 10 and 18).
