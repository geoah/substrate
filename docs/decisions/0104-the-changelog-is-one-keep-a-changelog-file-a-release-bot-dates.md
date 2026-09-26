---
status: proposed
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0104. The changelog is one Keep a Changelog file, and a release bot dates it

## Context and Problem Statement

People and agents upgrading a substrate need to know what changed and what
to do about it, and 76 of 300 commits on main were breaks. The first answer
(PR #655) was one fragment file per change under `docs/changes/`, placed in a
release by git and rendered into the GitHub release pages and
`mise run changelog`. Reviewed against
[Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), it breaks the
convention five ways: there is no changelog file to find, GitHub Releases are
the primary surface, release pages carry commit lists, only breaks and some
features get an entry, and the categories are not the six the convention
names. It also needs a renderer, page markers, a sync workflow, a `release:`
key and lint rules to stand in for one file.

A single file has one hard part here: every green merge to main releases
itself, and only a pull request may write to main, so nothing can move the
`Unreleased` entries under a version when a release is cut. A merge queue
cannot do it: GitHub offers none on a repository owned by a personal account,
and a merge queue merges only what it tested, adding no commits.

## Considered Options

- Keep the fragments under `docs/changes/`, and serve them from the server.
- One `CHANGELOG.md`, dated by a release pull request that a person merges to
  cut each release.
- One `CHANGELOG.md`, dated by a release bot that pushes to main after each
  release, through a GitHub App that is the ruleset's only bypass actor.
- One `CHANGELOG.md`, dated by a bot pull request that auto-merges.

## Decision Outcome

Chosen: one `CHANGELOG.md` at the repository root in the Keep a Changelog
1.1.0 format, dated by a release bot that pushes to main.

The file holds `## [Unreleased]` on top, then every version newest first as
`## [vX.Y.Z] - YYYY-MM-DD`, with entries under `Added`, `Changed`,
`Deprecated`, `Removed`, `Fixed` and `Security`. A break is an entry under
`Changed` or `Removed` that starts with `**Breaking:**` and carries the
upgrade steps as sub-bullets, exact enough for an agent to follow. A pull
request titled `feat`, `fix` or with a `!` adds its entry under
`[Unreleased]`; `commits:check` refuses one that does not.

A release still happens on every green merge. The version job tags the green
commit S as today, and the GitHub release body is S's `[Unreleased]` section,
with no commit list. The release bot then moves exactly the entries that were
under `[Unreleased]` at S into a new `## [vX.Y.Z] - YYYY-MM-DD` section on the
current main, leaves any entry added since S where it is, and pushes
`chore(release): changelog for vX.Y.Z`, retrying on a newer main. A `chore:`
commit bumps nothing, so the push cuts no release of its own. At the tag, the
source tree's `[Unreleased]` section is exactly that release, which is what a
server embedding the file reports as its own version.

A release pull request was rejected because a person would have to merge one
to release, which ends release-on-merge. A bot pull request was rejected
because it adds one pull request per release, several a day, lands a minute
and a half later, and has to be rebuilt whenever another merge conflicts with
it first. The fragments were rejected for the five breaks above.

### Consequences

- Good, because the changelog is one file with the conventional name, readable
  offline, in any checkout and at any tag.
- Good, because every `feat`, `fix` and break gets an entry, not only breaks.
- Good, because the fragments, `.mise/changelog.sh`, `.mise/releasenotes.sh`,
  the `release-notes` workflow, the `release:` key and the fragment lint rules
  are deleted.
- Bad, because the GitHub App can push anything to main. The job that holds
  its key refuses to push a commit that changes any file but `CHANGELOG.md`,
  and the key lives in a protected GitHub environment.
- Bad, because two open pull requests that both add under `[Unreleased]`
  conflict, and the second to merge rebases. `CHANGELOG.md merge=union` in
  `.gitattributes` keeps both sides on a local rebase.
- Bad, because between a release and the bot's push, main still lists that
  release's entries under `[Unreleased]`.

### Confirmation

`mise run commits:check` refuses a `feat`, `fix` or `!` pull request that adds
no line under `[Unreleased]`. `mise run lint:docs` holds the file's shape: the
section order, the version headings and the six category names. The release
bot's job refuses to push a commit that touches any file but `CHANGELOG.md`.

## More Information

The notes under `docs/changes/` move into `CHANGELOG.md` under the version
each shipped in (its `release:` key, else the first tag containing the commit
that added it), and every version without a note keeps its
`feat:` and `fix:` subjects as entries, so every version has one. The
fragment design this replaces was PR #655; the follow-up work, including
serving the file from the server, is geoah/substrate#681. Revisit if the
repository moves to an organization, where a merge queue could run the slow
suites before a merge, or if releases stop happening on every merge.
