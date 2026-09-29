---
status: proposed
date: 2026-09-29
decision-makers: George Antoniadis
---

# 0104. A release is a pull request that dates the changelog, and a merge to main releases nothing

## Context and Problem Statement

People and agents upgrading a substrate need to know what changed and what
to do about it. The first answer (PR #655) was one fragment file per change
under `docs/changes/`, rendered onto GitHub release pages. Reviewed against
[Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/) it breaks the
convention five ways: no changelog file to find, GitHub Releases as the
primary surface, commit lists on release pages, entries for breaks and some
features only, and categories that are not the six the convention names.

The second answer (this record as drafted on 2026-09-26) kept release on
every green merge and added a GitHub App that pushed a dated `CHANGELOG.md`
to `main` after each release, because with a release on every merge nothing
else could move the `[Unreleased]` entries under a version.

Release on every merge is itself the problem now. `main` carries 163 tags,
eight of them cut on 2026-09-29 alone, and the one deployment (`geoah.me`)
upgrades through a runbook that takes a backup first and reads every
release's notes in the range. A release nobody deploys is noise, an upgrade
that spans twenty releases reads twenty sections, and a version exists before
anybody has run the tree it names against real data. The owner wants a
release to be a decision with a QA step in front of it.

## Considered Options

- Keep release on every green merge, with the GitHub App dating the file
  afterwards (the 2026-09-26 draft).
- A release is a pull request titled `chore(release): vX.Y.Z` that dates
  `CHANGELOG.md`; merging it is what tags and publishes.
- A `workflow_dispatch` that tags `main` where it stands, with a bot dating
  the file afterwards.
- Release branches per minor, with fixes cherry-picked onto them.

## Decision Outcome

Chosen: a release is a pull request that dates the changelog. A merge to
`main` that is not a release pull request tags nothing and publishes nothing,
whatever its title says. `latest` keeps moving with `main` and is the build
the release is tested as.

The file is one `CHANGELOG.md` at the repository root in the Keep a Changelog
1.1.0 format: `## [Unreleased]` on top, then every version newest first as
`## [vX.Y.Z] - YYYY-MM-DD`, with entries under `Added`, `Changed`,
`Deprecated`, `Removed`, `Fixed` and `Security`, each entry ending with its
pull request number. A break is an entry under `Changed` or `Removed` that
starts with `**Breaking:**` and carries its upgrade steps as sub-bullets,
exact enough for an agent to follow. A pull request with `!` in its title
must add one; `commits:check` refuses it otherwise. A `feat` or `fix` adds an
entry when its title does not explain the change, and is otherwise left to
the release step.

`mise run release:prepare [vX.Y.Z]` cuts a release. It takes the version from
`svu next --v0` unless one is given, adds an entry from the subject of every
`feat:` and `fix:` commit since the last tag whose pull request no entry
names, moves the whole `[Unreleased]` section under the new heading with
today's date, commits `chore(release): vX.Y.Z` on `release/vX.Y.Z`, pushes,
and opens the pull request with the QA checklist as its body. The person
runs the checklist against `latest`, edits the entries, and merges. Merging
a release pull request is releasing: the `version` workflow, still hanging
off a green `ci` run on `main`, tags a commit only when its subject is
`chore(release): vX.Y.Z`, that version is the file's newest heading,
`[Unreleased]` is empty, and no such tag exists. The GitHub release body is
that version's section, with no commit list.

A merge to `main` after the release pull request opens conflicts with it, and
the pull request is prepared again, so a release is exactly what its section
lists. Nothing pushes to `main` outside a pull request, so the ruleset keeps
no bypass actor and no GitHub App exists.

The 2026-09-26 draft was dropped because its whole mechanism, the App, the
bypass actor and the protected key, existed to work around release on merge,
which is no longer wanted. A dispatch that tags `main` where it stands was
rejected because the version would exist before its notes were dated and
reviewed, which is the QA step in the wrong order. Release branches were
rejected because there is one deployment and it always takes `main`; a
fix that production needs is released the same way, with whatever else
`main` holds.

### Consequences

- Good, because a release is a reviewed pull request: the notes are read, the
  build is run and the version is chosen before anything is tagged.
- Good, because the changelog is one file with the conventional name, and
  every `feat`, `fix` and break has an entry, written by the author or taken
  from its commit subject.
- Good, because no GitHub App, bypass actor, protected environment or
  bot push exists, and the fragments, `.mise/changelog.sh`,
  `.mise/releasenotes.sh`, the `release-notes` workflow and the fragment lint
  rules are deleted.
- Bad, because a release is now a chore somebody has to do, and `main` can
  carry unreleased fixes for days. The owner accepts that: it is the point.
- Bad, because two open pull requests that both add under `[Unreleased]`
  conflict, and the second to merge rebases.
- Bad, because a release pull request goes stale the moment another pull
  request merges, and is prepared again.

### Confirmation

`mise run commits:check` refuses a `!` pull request that adds no
`**Breaking:**` entry under `[Unreleased]`, and a `chore(release): vX.Y.Z`
pull request whose diff touches any file but `CHANGELOG.md`, whose version
is not the file's newest heading, or that leaves `[Unreleased]` non-empty.
`mise run lint:docs` holds the file's shape: the section order, the version
headings and the six category names. The `version` workflow tags nothing but
a release commit, and the `release` workflow has no other door.

## More Information

The notes under `docs/changes/` move into `CHANGELOG.md` under the version
each shipped in (its `release:` key, else the first tag containing the commit
that added it), and every version without a note keeps its `feat:` and
`fix:` subjects as entries, so every version has one. The design and the
checklist are in [the releases plan](../plans/releases.md); the
implementation is geoah/substrate#681, and this record is accepted with it.
Revisit if a second deployment appears that cannot take `main`, which is
when a release branch starts earning its keep, or if the release chore is
skipped for so long that `[Unreleased]` stops being readable.
