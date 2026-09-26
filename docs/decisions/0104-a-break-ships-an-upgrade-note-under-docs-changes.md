---
status: proposed
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0104. A break ships an upgrade note under docs/changes

## Context and Problem Statement

A release's notes are goreleaser's list of `feat:` and `fix:` subjects
(`.goreleaser.yaml`, `changelog.use: git`). 76 of the last 300 commits on
main were breaks, and a subject such as `feat(oauth)!: refuse a bare account
id on oauth/start` tells a client that something broke, not what to send
instead. The steps lived in PR bodies and decision records, which a person or
an agent upgrading a deployment does not read. `CHANGELOG.md` was deleted in
2d24fcc5 because nothing wrote it.

## Considered Options

- Keep the commit list, and put the migration steps in the commit body.
- One hand-kept `UPGRADING.md` with an "Unreleased" section each PR edits.
- One file per change under `docs/changes/`, placed in a release by the tag
  range that added it, rendered into the release notes.

## Decision Outcome

Chosen: one file per change under `docs/changes/`. Each note carries a
`type:` (`breaking`, `deprecated`, `feature`, `fix`), one heading, an exact
example and, for a break or a deprecation, a `## What to do`. The release job
renders the notes the tag's range added above goreleaser's commit list
(`.mise/changelog.sh --release`), and `mise run changelog` renders every
release. A `!` or a `BREAKING CHANGE:` footer with no `type: breaking` note
added on the branch is refused.

A commit body is not reviewable as a file, and a squash merge concatenates
every commit's body into it. A single file with an "Unreleased" section
conflicts on every pair of open PRs, and moving that section under a version
at release time needs a commit to main, which the ruleset allows only through
a pull request.

### Consequences

- Good, because the notes are files: `lint:docs` holds their shape, the
  agent review reads them against the diff, and a checkout has them offline.
- Good, because a note's release is derived from git, so nobody edits a
  version into it and it cannot name the wrong one.
- Bad, because a note is placed by the commit that ADDED it: renaming or
  deleting one after merge moves or drops it from the rendered history.
- Bad, because only breaks are enforced. A deprecation or a feature that
  needs a note is caught by the agent review's comment or not at all.
- Bad, because releases before this record have no notes; their breaks are
  the commit subjects alone.

### Confirmation

`mise run commits:check` (the `conventional commits` check in
`.github/workflows/pr.yml`, required by `.github/rulesets/main.json`) refuses
a break without a note. `mise run lint:docs` refuses a note in the wrong
shape. The agent review in `.github/workflows/review.yml` is advice only.

## More Information

The format and the rules for when a note is required are
[docs/changes/README.md](../changes/README.md). Revisit if releases move to a
release pull request, which would make a single rendered file possible
without a bypass on main.
