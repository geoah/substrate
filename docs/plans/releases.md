# Plan: a release is a pull request that dates the changelog

Status: design settled with the owner 2026-09-29, recorded as
[decision record 0104](../decisions/0104-a-release-is-a-pull-request-that-dates-the-changelog.md).
Implementation is [#681](https://github.com/geoah/substrate/issues/681). It
is a plan, not a contract: the code that lands is the contract, and
`docs/operations.md` describes the flow once it does.

## The problem

Every green merge of a `feat:` or `fix:` to `main` tags a version and
publishes it. That gave `main` 163 tags, eight on one day, and a version
exists before anybody has booted the tree it names against real data. The
one deployment upgrades through a runbook that backs up first and reads every
release's notes in the range, so twenty small releases cost twenty sections
and one long read. The upgrade notes themselves are fragments under
`docs/changes/`, found through GitHub release pages and `mise run changelog`
and nowhere else.

What the owner wants: merge to `main` freely, cut a release on purpose, have
one `CHANGELOG.md` that a release updates with everything since the previous
one, and run a QA step before the version exists.

## The design

### The file

`CHANGELOG.md` at the repository root, in
[Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/):

```markdown
# Changelog

## [Unreleased]

### Fixed

- Refuse a blob read whose bytes do not hash to the digest (#763)

## [v0.111.0] - 2026-09-29

### Changed

- **Breaking:** `substrated` binds `127.0.0.1` and refuses a non-loopback
  bind (#778)
  - Put a reverse proxy in front, or set `SUBSTRATE_BIND` to the address the
    proxy reaches.

### Added

- `SUBSTRATE_TRIGGER_INTERVAL` sets the dispatcher tick (#758)

[Unreleased]: https://github.com/geoah/substrate/compare/v0.111.0...HEAD
[v0.111.0]: https://github.com/geoah/substrate/compare/v0.110.6...v0.111.0
```

- `## [Unreleased]` first, then every version newest first as
  `## [vX.Y.Z] - YYYY-MM-DD`.
- The six categories, in this order where present: `Added`, `Changed`,
  `Deprecated`, `Removed`, `Fixed`, `Security`.
- One entry per line, stating the change the way a commit subject does, with
  every route, flag, env var and kind reference in backticks, ending with
  the pull request number in parentheses. The number is what
  `release:prepare` matches against the commits, so it is not decoration.
- A break is an entry under `Changed` or `Removed` starting with
  `**Breaking:**`, followed by sub-bullets with the steps that take a client
  or a deployment from the old behavior to the new, in order. An agent will
  follow them literally, so show the command or the request.
- The compare links at the bottom, one per heading.

`mise run lint:docs` holds the shape: the heading order, the version heading
form, the category names and order, one entry per line, a `**Breaking:**`
entry with at least one sub-bullet, and a compare link for every heading.

### The entry a pull request adds

A pull request adds under `[Unreleased]` when the change is one a user of a
substrate has to act on or would not find from the title:

- **A break** (`!` in the title or any subject, or `BREAKING CHANGE:` in a
  body) must add a `**Breaking:**` entry with its steps. `commits:check`
  refuses the pull request without one, as it refuses a missing
  `type: breaking` fragment today.
- **A deprecation** adds under `Deprecated`.
- **A feature or a fix** adds an entry when its title is not enough to use
  it: a new env var, a new CLI verb, a behavior an agent should start relying
  on. Most do not, and the release step writes their line from the commit
  subject.

Two open pull requests that both add under `[Unreleased]` conflict, and the
second to merge rebases. The entries are one line each, so the rebase is a
line. No `merge=union` attribute: a union merge would silently keep a stale
`[Unreleased]` entry beside a release that moved the rest.

### Preparing a release

```
mise run release:prepare            # version from svu next --v0
mise run release:prepare v0.112.0   # a chosen version
```

The task, `.mise/releaseprepare.sh`, runs on a clean checkout of `main` and:

1. Refuses when `[Unreleased]` is empty and no `feat:` or `fix:` commit
   exists since the last tag: there is nothing to release.
2. Takes the version from the argument, else `svu next --v0` (a `fix:` bumps
   the patch, a `feat:` the minor, a break the minor while below 1.0.0).
   Refuses a version at or below `svu current` or one that already has a tag.
3. Lists every `feat:` and `fix:` commit since the last tag, reads the
   `(#N)` its squash subject carries, and for each one that no entry under
   `[Unreleased]` names, adds a line from the subject: `feat` under `Added`,
   `fix` under `Fixed`, a `!` under `Changed` as `**Breaking:**` with a
   placeholder sub-bullet the person must replace. A commit whose pull
   request an entry already names adds nothing, so a hand-written entry wins
   over the generated one.
4. Moves the whole `[Unreleased]` section under `## [vX.Y.Z] - YYYY-MM-DD`,
   leaves `## [Unreleased]` empty above it, and rewrites the compare links.
5. Commits `chore(release): vX.Y.Z` on `release/vX.Y.Z`, pushes, and opens the
   pull request with `gh pr create`, the title as the commit subject and the
   body from `.github/release_pull_request.md`: the QA checklist below with
   the version, the previous tag and the compare link filled in.

The task runs on a laptop with the person's own `gh` login. A
`workflow_dispatch` that runs the same script from CI is possible later, but
a pull request opened with the workflow token starts no checks of its own, so
it would need a token of a person or an App. Not in the first cut.

### The release pull request and its checks

The pull request touches `CHANGELOG.md` and nothing else. `commits:check`,
seeing a `chore(release): vX.Y.Z` title, holds:

- the diff names only `CHANGELOG.md`;
- `vX.Y.Z` is the file's newest version heading, dated today or earlier;
- `[Unreleased]` is empty;
- no `**Breaking:**` entry still carries the placeholder sub-bullet;
- no tag `vX.Y.Z` exists.

The body is the checklist, and the person ticks it before merging:

```markdown
## Release vX.Y.Z

Previous: vP.Q.R. Changes: https://github.com/geoah/substrate/compare/vP.Q.R...main

- [ ] The section above lists every `feat:` and `fix:` since vP.Q.R
      (`git log vP.Q.R..main --oneline`), and every break carries its steps.
- [ ] The version is right: a break bumps the minor while below 1.0.0, and a
      chosen version is stated here with why.
- [ ] `latest` is this tree: `GET /.well-known/substrate/server.json` on
      `ghcr.io/geoah/substrate:latest` reports `vP.Q.R-N-g<sha>` where `<sha>`
      is this pull request's base.
- [ ] `mise run image:smoke ghcr.io/geoah/substrate:latest` passes.
- [ ] Booted `latest` against a copy of a production snapshot, read the boot
      upgrade log, and `substratectl repository verify` passed
      (the upgrade drill in `docs/operations.md`).
- [ ] The runbook skill still reads this release's section as written.
```

The tree the release builds is the release commit, whose only difference
from the `latest` that was tested is `CHANGELOG.md` and the version stamp.

A merge to `main` while the release pull request is open conflicts with it
(both sides edit the `[Unreleased]` region). The fix is
`git branch -D release/vX.Y.Z` and `mise run release:prepare` again on the
new `main`, so the release lists exactly what it ships.

### Tagging and publishing

`.github/workflows/version.yml` keeps its trigger (a completed `ci` run on
`main`, plus the `workflow_dispatch` recovery path that first proves a green
run exists) and its concurrency, and changes what it tags. It tags the green
commit only when all of these hold, and otherwise exits saying which one did
not:

- the commit's subject is `chore(release): vX.Y.Z`;
- `CHANGELOG.md` at that commit has `vX.Y.Z` as its newest version heading
  and an empty `[Unreleased]`;
- no tag `vX.Y.Z` exists, or it exists at this commit and has no GitHub
  release yet (the half-finished run, as today).

A `feat:` or `fix:` merge that is not a release commit tags nothing. `svu`
is no longer read here; it is read by `release:prepare`.

`.github/workflows/release.yml` keeps its one door and its checks. Two
changes: goreleaser's `changelog:` is disabled, and `--release-notes` is the
release's section, cut from `CHANGELOG.md` by `.mise/changelogsection.sh
vX.Y.Z`. The release page is that section and nothing else.

`.github/workflows/latest.yml` does not change. `latest` is the tip of
`main`, the build every release pull request tests.

Recovery when a release commit landed on `main` with a non-empty
`[Unreleased]` (a race the check above did not see): the `version` job
refuses to tag and says so. A second pull request titled
`chore(release): vX.Y.Z`, moving the stray entries under the same heading,
satisfies the rule and the tag lands on that second commit.

### Cadence and QA

There is no calendar. A release is cut when there is something to deploy: the
owner wants `geoah.me` on a fix or a feature, or an outside user asks for
one. The soft rule is that `[Unreleased]` should stay readable in one screen;
when it does not, cut a release. A fix that production needs is released the
same way and ships whatever else `main` holds; there is no release branch,
because one deployment takes `main` and there is nothing to backport to.

The QA is the checklist. Its two real steps are the image smoke and the
upgrade drill: boot the candidate against a copy of the newest production
snapshot and read what the boot upgrade did. `docs/operations.md` gets an
"Upgrade drill" section that says how, using the snapshot and restore steps
already on that page. Automating the drill (`mise run release:drill
<snapshot>`) is later work, once the manual steps have been run a few times
and stopped changing.

## What changes in the tree

Added:

- `CHANGELOG.md`, with the history folded in: every note under
  `docs/changes/` under the version it shipped in, and every version since
  v0.70.0 without a note keeping its `feat:` and `fix:` subjects as entries.
- `.mise/releaseprepare.sh`, `.mise/changelogsection.sh`, and the tasks
  `release:prepare` and `changelog:section` in `.mise.toml`.
- `.github/release_pull_request.md`, the checklist body.
- A `CHANGELOG.md` section in `.mise/docscheck.sh` (the shape rules above).
- A `chore(release)` branch in `.mise/commitscheck.sh` (the pull request
  rules above), and the `**Breaking:**` entry rule replacing the
  `type: breaking` fragment rule.
- An "Upgrade drill" section in `docs/operations.md`.

Changed:

- `.github/workflows/version.yml`: tags release commits only.
- `.github/workflows/release.yml` and `.goreleaser.yaml`: the release body
  is the section, no commit list.
- `AGENTS.md` ("the title is the release" becomes "the title is the version
  bump, and a release is a pull request"), `docs/for-agents.md`,
  `docs/operations.md` "Upgrading the binary", `docs/README.md`, `README.md`
  "Upgrading", `.github/pull_request_template.md`, `.github/review.md` and
  `skills/substrate-runbook-upgrade/SKILL.md` point at `CHANGELOG.md` on
  `main`, one section per version in the range. The skill reads the file,
  not release pages.
- Decision record 0104 becomes `accepted`.

Deleted:

- `docs/changes/` (after the fold), `.mise/changelog.sh`,
  `.mise/releasenotes.sh`, `.github/workflows/release-notes.yml`, the
  `changelog` and `release:notes` tasks, and the fragment rules in
  `.mise/docscheck.sh`.

## Phases

1. **The file and the flow.** `CHANGELOG.md` with the folded history,
   `release:prepare`, the `version` and `release` workflow changes, the
   checks, the checklist, the docs. The fragments stay one more release so
   the fold can be checked against them. Cut the first release with the new
   flow.
2. **Delete the fragments** and everything that rendered them. Accept 0104.
3. **Serve the file.** `GET /.well-known/substrate/changelog` returns the
   embedded `CHANGELOG.md`, so an agent with only a server URL reads the
   section for the version it runs. The runbook skill reads it from there
   first and from `main` second.

## Rejected

- **Release on every merge with a bot dating the file** (the 2026-09-26
  draft of 0104): the App, the bypass actor and the protected key existed
  only to work around release on merge.
- **A dispatch that tags `main` where it stands**: the version exists before
  its notes are dated and reviewed.
- **Release branches**: one deployment, always on `main`; revisit when a
  second one cannot take it.
- **Generating the whole changelog from commits at release time**, with no
  entry from any pull request: the author of a break is the one who knows
  its upgrade steps, and reconstructing them at release time is how the
  steps end up wrong.
