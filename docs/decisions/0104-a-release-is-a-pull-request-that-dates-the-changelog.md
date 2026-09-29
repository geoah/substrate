---
status: accepted
date: 2026-09-29
decision-makers: George Antoniadis
---

# 0104. A release is a pull request that dates the changelog, and a merge to main releases nothing

## Context and Problem Statement

People and agents upgrading a substrate need to know what changed and what
to do about it. The first answer (PR #655) was one fragment file per change
under `docs/changes/`, rendered onto GitHub release pages: no changelog file
to find, release pages as the primary surface, entries for breaks and some
features only. The second answer (this record as drafted on 2026-09-26) kept
release on every green merge and added a GitHub App that pushed a dated
`CHANGELOG.md` to `main` after each release, because with a release on every
merge nothing else could move the unreleased entries under a version.

Release on every merge is itself the problem. `main` carries 162 tags, nine
cut on 2026-09-29 alone, and the one deployment upgrades through a runbook
that takes a backup first and reads every release in the range. A release
nobody deploys is noise, and a version exists before anybody has run the tree
it names against real data. The owner wants a release to be a decision with
a QA step in front of it, and one changelog file that a release updates with
everything since the previous one.

## Considered Options

- Keep release on every green merge, with the GitHub App dating the file
  afterwards (the 2026-09-26 draft).
- A release pull request a person prepares by hand with a script, that
  moves the unreleased entries under a version and opens the pull request.
- A release pull request that release-please keeps open and regenerates after
  every merge, from the conventional commits since the last tag; merging it
  tags and publishes.
- A `workflow_dispatch` that tags `main` where it stands.
- Release branches per minor, with fixes cherry-picked onto them.

## Decision Outcome

Chosen: the release pull request release-please keeps. A merge to `main`
tags nothing and publishes nothing, whatever its title says; `latest` keeps
tracking `main` and is the build the release is tested as. After every green
`ci` run on `main`, release-please regenerates one pull request,
`chore(release): vX.Y.Z`, whose branch holds the next `CHANGELOG.md` section
and the next version, both read from the conventional commits since the last
tag. A person runs the checklist in `docs/releasing.md` against it and
merges. The next run tags the merge, creates the GitHub release with the
section as its body, and calls `release.yml`, which checks the tagged
commit's own green `ci` run and attaches the artifacts.

The changelog is release-please's: one `CHANGELOG.md` at the root, a
`## [X.Y.Z](compare) (date)` heading per release, the sections
`⚠ BREAKING CHANGES`, `Added` and `Fixed`, one line per merged title. A break
carries its upgrade steps in a `BREAKING CHANGE:` footer on a commit of the
branch, and that text is the entry; `commits:check` refuses a `!` without
one. A wrong line is fixed by editing the merged pull request's body
(`BEGIN_COMMIT_OVERRIDE`), never the file. The breaking notes under
`docs/changes/` were folded into the file as entries with their steps under
the version each shipped in, every version since v0.70.0 keeps its `feat:`
and `fix:` subjects as lines, and nothing before v0.70.0 is listed: one line
per change, so the file stays a list and not a book.

The hand-run script was rejected because release-please already does the
same and keeps the pull request current without anybody running anything:
the pull request is always there to merge. The 2026-09-26 draft was dropped
because its App, bypass actor and protected key existed only to work around
release on merge. A dispatch that tags `main` where it stands was rejected
because the version would exist before its notes were reviewed. Release
branches were rejected because there is one deployment and it always takes
`main`.

### Consequences

- Good, because a release is a reviewed pull request that always exists:
  the notes are read and the build is drilled before anything is tagged, and
  nobody prepares anything.
- Good, because every `feat`, `fix` and break has a line, the file has the
  conventional name, and it reads offline in any checkout.
- Good, because the fragments, their renderer, the release-notes sync
  workflow, `svu` and the fragment lint rules are deleted, and no GitHub App
  or bypass actor exists.
- Bad, because the file's headings are release-please's, not Keep a
  Changelog's, and its sections are the ones its config names.
- Bad, because the workflow needs a personal access token: a pull request
  the workflow token opens gets no checks and cannot be merged under the
  ruleset. The token has no bypass.
- Bad, because a break's steps live in a commit footer, where an author
  writes less than in a file. The agent review reads every pull request for
  a footer that is missing or wrong.
- Bad, because a release is now a chore somebody has to do, and `main` can
  carry unreleased fixes for days. That is the point.

### Confirmation

`mise run commits:check` refuses a `!` or a `BREAKING CHANGE:` footer with no
step text on any commit, and a `chore(release): vX.Y.Z` pull request that
touches any file but `CHANGELOG.md` and the manifest; `.mise/cicheck.sh`
holds those scenarios. `release.yml` has one door, the release-please
workflow, and refuses a tagged commit with no green `ci` run of its own.

## More Information

The flow, the checklist and the setup are in `docs/releasing.md`; the
implementation was geoah/substrate#681. Revisit if a second deployment
appears that cannot take `main`, which is when a release branch starts
earning its keep, or if release-please's format stops being enough for the
steps a break needs.
