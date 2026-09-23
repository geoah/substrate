---
status: accepted
date: 2026-09-23
decision-makers: George Antoniadis
---

# 0099. A repository migration is code the boot runs once and records

## Context and Problem Statement

Record [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md) refuses
a bare name in a stored declaration, and every repository created before it
holds some: core's own `recordsplit.merge` pins `recordmerge` bare. The
open reads the stored closure before the shipped upgrade runs, so those
repositories refuse to open under the new binary, and nothing in the tree
can repair them. A SQL migration (`internal/engine/migrations/`) cannot: the
answer is per repository, decided from its rows (which `person` the old
loader resolved a word to), and the repair has to be ordinary record writes
through the changelog so a rebuild reproduces it. The same shape will recur
whenever a release changes what stored rows may say.

## Considered Options

- Special-case the rewrite inside the stored vocabulary load.
- Spend a vocabulary dialect number and rewrite in the dialect gate.
- A general runner: per-repository code migrations with their own ledger,
  shaped like the schema runner.

## Decision Outcome

Chosen: the general runner, `internal/engine/repomigrate.go`. Each migration
has a version and a name, lives in its own file
`repomigration_NNNN_name.go`, and runs at a repository's first open under a
binary that carries it, before the stored vocabulary loads, in one
transaction with the ledger row it leaves in `repository_migrations`
(`repository`, `version`, `name`, `applied_at`). The ledger is checked as
`schema_migrations` is: a recorded version the binary does not carry
(`ErrRepositoryMigrationsNewer`), a recorded name that differs, or a pending
version below one already recorded refuses that repository's open by name,
and the API answers `503`. A landed migration is never edited; `frozen:check`
and `lint:migrations` hold the files as they hold the SQL ones.

Three rules bind a migration. It writes through the changelog as ordinary
record writes, never into the fold, and what it changes about a declaration
it re-derives for the rows the declaration describes (the refs index, the
search bands), because a boot import folds those rows before it runs. It
runs before the stored vocabulary loads, so it works from the rows and the
binary's own registry alone. It is idempotent, because the ledger is the
database's: a repository directory imported into a fresh database carries
its changelog and not the ledger, so every migration runs once more there
and must find nothing to do.

The import path is the one this is for as much as the upgrade in place. A
directory restored under a fresh database is folded from its segments at
boot under whatever of its closure this binary admits, and the migrations
run at its first open, over rows the source installation may never have
migrated. The rewrite lands in this installation's changelog for the
repository, and the ledger row in this database.

The special case lost because the next rewrite would add a second one, and
the load would become the place every repair hides. The dialect gate lost
because a dialect number names the SHAPE of the rows, and a spelling change
inside a value leaves the shape as it was; spending one for it would make an
older binary refuse rows it can read.

Migration 1, `qualify_bare_declaration_names`, writes the full identity into
every stored `kind:` pin, `trait:` pin, `traits:` binding and function
allowlist entry, resolving each word the way the binary before 0098 did:
the declaring package, then core for a trait, then the one declaration
anywhere carrying the word. A word that rule could not resolve stays bare,
and its package parks with the reason, as it did before. Each row keeps its
stored version, and an imported sample's origin digest is recomputed over
the rewritten rows so the copy reads pristine.

### Consequences

- Good, because a repository from before 0098 opens under this binary with
  nothing recreated, and every later rewrite of stored rows has one place to
  go.
- Good, because the ledger and its refusals are the ones an operator already
  reads for the schema.
- Bad, because a migration runs before the vocabulary loads, so it cannot use
  the API or the admission door and works at the level of documents and the
  projection.
- Bad, because a fresh-database import runs every migration once more, so
  each one pays its scan even when there is nothing to do.
- Bad, because a migration that fails fails the repository's open, with no
  way past it short of a binary that carries a fix.

### Confirmation

`internal/engine`'s `TestRepositoryMigrationQualifiesStoredBareNames` plants
bare names into stored declarations, reopens, and holds the rewrite, the
ledger row, the preserved versions, the second open's silence and a rebuild;
`TestRepositoryMigrationRunsOnAnImportedDirectory` copies a directory with
bare declarations under a fresh database and holds the import, the
migration, the refs index, the search bands and a lexical search against the
source; `TestRepositoryMigrationsRefuseADivergentLedger` holds the three
refusals; `TestRepositoryMigrationsMatchTheirFiles` holds the runner's list
to the files; `mise run lint:migrations` and `mise run frozen:check` hold
the files.

## More Information

Rests on [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md), which
this repairs, and on
[0063](0063-a-property-rename-is-ordinary-record-writes.md),
[0066](0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md) and
[0078](0078-a-kind-move-is-ordinary-record-writes.md) for the rule that a
rewrite of stored data is ordinary record writes. Reopen if the ledger has
to travel with the repository directory, which means a manifest format
change and a decision of its own.
