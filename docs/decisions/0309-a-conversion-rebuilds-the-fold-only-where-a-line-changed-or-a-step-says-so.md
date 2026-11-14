---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0309. A conversion rebuilds the fold only where a line changed or a step says so

## Context and Problem Statement

[0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)
says the converted lines are taken "in one transaction with the new stamp
and the import-progress marker, the converted directory replaces the live
one, and the fold is rebuilt". The marker and the rebuild are there because
the fold was folded from the old lines: once a step rewrites them, the rows
no longer match their history. The code did both for every raise of the
dialect, whether or not a step changed a line of the repository.

The rebuild is not cheap. On a test machine (a Linux container on a laptop,
Postgres in a container beside it), a history of 502,979 lines (1.41 GB):
one walk of a step over it took about 85 seconds (about 5,900 lines a
second), `repository rebuild` took 386 seconds (about 1,300 lines a second),
and the boot's rebuild after a conversion ran at 75 falling to 45 lines a
second, 335,500 lines in 93 minutes before it was stopped. That is as
measured there, not a promise, and the gap between the last two is
unexplained yet. The boot's rebuild replays the history twice before the
repository serves: once under an empty registry, then under the real one.

`main` has raised the changelog dialect four times since v0.123.1 (steps 2
to 5), and steps of the 1.0 plan still to land raise it without rewriting a
line. Under 0260 each of them would make every repository replay its whole
history twice before serving, to arrive at the fold it already had.

## Considered Options

- Rebuild after every raise, as 0260 says.
- Rebuild only where a step changed a line.
- Rebuild where a step changed a line, or where a step says the fold must be
  rebuilt.

## Decision Outcome

Chosen: the third. The boot rebuilds the fold after a conversion only where
a step changed a line of that repository, or where a pending step says
`Refolds`: its carry changes a row the fold is derived from, or it comes with
a fold table whose content the lines already held, which the schema migration
beside it left empty. Otherwise the conversion raises the stamp and does
nothing else. The marker is set only in those two cases, and a marker already
set (an import, a restore, or a conversion a start was cut short in) stays
set, so the rebuild that marker asks for still happens.

`Refolds` is a field on the step, not a comment. Of today's steps only 5
(`marks`) sets it: SQL migration 0026 created `property_marks` empty beside
lines that held `manager` effects, and re-keyed the derived offers. Steps 2, 3 and
4 rewrite lines and have no carry.

The second option was not enough: step 5 shows that a step can need a rebuild
when it brings a fold table, even where its line rewrite has nothing left to
change.

### Consequences

- Good, because a raise that rewrites nothing costs one walk of the history
  per pending step and no replay, and a repository already converted and
  refolded by an earlier build replays nothing.
- Good, because a start cut short after such a raise leaves nothing to finish.
- Bad, because a step that changes how existing lines fold must now say so.
  One that does not is not refolded where its line rewrite changes nothing,
  and that repository serves the old fold.
- Bad, because the test below runs under this binary only, so it compares two
  folds this binary made. A step whose fold table is not named in
  `keptStepFoldTables`, or which changes how `fold.go` reads a line it leaves
  untouched, is held by review. Beyond that, the only check is the kept
  comparison against what a release served.

### Confirmation

`internal/engine/conversion_refold_internal_db_test.go` covers five cases: a
raise that changes no line keeps the fold rows (the same `ctid` and `xmin`)
and sets no marker; a step that says `Refolds` rebuilds; a step that changes
a line rebuilds; a start cut short after the commit leaves nothing to finish;
and a restore through a raise keeps its marker and folds.

`internal/testenv/keptrefold_db_test.go` works on the kept v0.123.1
directory. It imports the directory, then starts the repository again, which
must replay nothing. Then, for every step in the chain, it lowers the stamp
below that step and empties the fold tables the step came with. The next start
must leave every line as it was, and its fold must equal what
`repository rebuild` makes of the history. With step 5's `Refolds` removed,
this test fails.

## More Information

This amends 0260's sentence quoted above, and nothing else in it.
