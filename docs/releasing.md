# Releasing

A merge to `main` releases nothing. The `latest` image tracks `main`
([the published image](operations.md#the-published-image)), and a version
exists only when somebody merges the **release pull request**. This page is
how that pull request is kept, how a change reaches `CHANGELOG.md`, and the
checklist a person runs before merging.

## The release pull request

[release-please](https://github.com/googleapis/release-please) keeps one
pull request open against `main`, titled `chore(release): vX.Y.Z`, and
labeled `autorelease: pending`. Its branch changes two files:
`CHANGELOG.md` gains the next release's section, and
`.release-please-manifest.json` records the version. Both are computed from
the conventional commits merged since the last tag:

| Title                        | Section            | Bump while below 1.0.0 |
| ---------------------------- | ------------------ | ---------------------- |
| `feat(scope): …`             | Added              | minor                  |
| `fix(scope): …`              | Fixed              | patch                  |
| `…!: …` or a footer          | ⚠ BREAKING CHANGES | minor                  |
| `docs`, `refactor`, `test`, `chore`, `ci` | none  | none                   |

The workflow `.github/workflows/release-please.yml` runs after every green
`ci` run on `main` and regenerates the pull request, so it always lists what
merging it would release. Nothing to release (only `docs:` and `chore:`
since the tag) means no pull request. `release-please-config.json` holds the
rules above.

Merging the pull request is the release. The next run tags the merge commit
`vX.Y.Z`, creates the GitHub release with the section as its body, swaps the
label to `autorelease: tagged`, and calls `.github/workflows/release.yml`,
which checks that the tagged commit's own `ci` run was green and then runs
`mise run release`: goreleaser builds the CLI archives and the multi-arch
image and attaches them to that release
([the published image](operations.md#the-published-image)).

## Cutting a release

Run this against the open `chore(release): vX.Y.Z` pull request. Nothing here
is automated on purpose: it is the QA step.

1. **Read the section.** Every `feat:` and `fix:` merged since the last tag
   is a line. Every break has its steps, exact enough to follow. A wrong or
   missing line is fixed on the merged pull request it came from (below),
   never in the release branch, which the next run regenerates.
2. **Check the version.** A break bumps the minor while below 1.0.0. To ship
   a different version, merge a commit whose body carries
   `Release-As: X.Y.Z`; release-please reopens the pull request at that
   version.
3. **Confirm `latest` is this tree.** The pull request's base is the tip of
   `main`, and `latest` is that build:
   `GET /.well-known/substrate/server.json` on
   `ghcr.io/geoah/substrate:latest` reports `vP.Q.R-N-g<sha>` where `<sha>`
   is the base commit.
4. **Smoke the image.** `mise run image:smoke ghcr.io/geoah/substrate:latest`
   boots it against a throwaway Postgres and checks it serves.
5. **Run the upgrade drill.** Restore the newest backup of a real
   deployment into a throwaway Postgres and data root
   ([backups](operations.md#backups)), start `latest` against the copy, read
   what the boot upgrade logged, and run `substratectl repository verify`
   for each repository. A refusal or a long migration here is a break the
   section has to name.
6. **Merge.** Squash, as any pull request. The release exists a few minutes
   later; `gh release view vX.Y.Z` shows the artifacts once `release.yml`
   finishes. Then upgrade the deployment through the runbook
   ([Upgrading](../README.md#upgrading)).

A pull request merged to `main` while the release pull request is open is
fine: the next run regenerates the release pull request with the new line.
Merge it whenever the section reads right.

## Writing for the changelog

The title is the line. `feat(api): add the window read` lands as
`* **api:** add the window read (#123)`, so a title says what the thing does
now, with every route, flag, env var and kind reference in backticks.

A break carries its upgrade steps in a `BREAKING CHANGE:` footer, in a
commit body on the branch. The footer's text is the entry under
⚠ BREAKING CHANGES, and a person or an agent will follow it literally:

```
feat(server)!: bind 127.0.0.1 and refuse a non-loopback bind

The server spoke plain HTTP on every interface.

BREAKING CHANGE: the server listens on 127.0.0.1 unless SUBSTRATE_BIND_ADDRESS
names another address, and refuses a non-loopback address unless
SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true. With ghcr.io/geoah/substrate and this
repository's compose.yaml, nothing to do: the image sets both. An image of
your own adds both variables to the service's environment. A bare binary
behind a proxy on another host sets SUBSTRATE_BIND_ADDRESS to the interface
the proxy reaches, and keeps every other peer off the port.
```

`mise run commits:check` (the `conventional commits` check) refuses a `!`
with no such footer on any commit of the branch, and a footer with no text.
The phrase mid-sentence in prose is prose; only a line starting with
`BREAKING CHANGE:` is the footer. A feature or a fix whose title is not
enough to use it (a new env var, a new CLI verb, a behavior an agent should
start relying on) says the rest in the commit body; most do not need to.

**Fixing a line after the merge.** Edit the body of the merged pull request
the line came from and add:

```
BEGIN_COMMIT_OVERRIDE
fix(cli): print apply progress to stderr during a vocabulary batch

BREAKING CHANGE: a script that treats any stderr output as a failure now
sees a progress line every 10 s on a slow apply that succeeds.
END_COMMIT_OVERRIDE
```

The next run reads the override in place of the squash commit's message and
regenerates the release pull request. This works for a squash merge, which
is how `main` takes every pull request.

## The file

`CHANGELOG.md` at the repository root, newest release first, one
`## [X.Y.Z](compare link) (date)` heading per release. release-please writes
it and inserts each new section above the previous one; nobody edits it by
hand. The file was folded on 2026-09-29 from the tags, the `feat:` and
`fix:` subjects between them, and the breaking notes that used to live as
one file per change under `docs/changes/`: each is a `**Breaking:**` entry
with its steps as sub-bullets. Releases before v0.70.0 are not listed, since
no database from before it can be upgraded in place. One line per change is
the rule; the file stays short because a release is a section, not a page.

## Setup and recovery

The workflow authenticates with `RELEASE_PLEASE_TOKEN`, a fine-grained
personal access token scoped to this repository with read and write on
contents and pull requests. Not the workflow's own token: a pull request
opened or pushed with `GITHUB_TOKEN` starts no workflow run, so the release
pull request would never get the checks the ruleset requires. The token has
no bypass on the ruleset; `main` still moves only through a pull request
that passed them. The two `autorelease:` labels are in `.github/labels.yml`,
which the labels sync holds the repository to.

A run that never happened (a dropped `workflow_run` event) or died after
`ci` went green is replayed with a `workflow_dispatch` of `release-please`;
it refreshes an open pull request, tags a merged one that was not, and
leaves a tagged one alone. A release whose artifacts failed to upload is
replayed with "Re-run failed jobs" on the `release-please` run that called
`release.yml`: goreleaser replaces the artifacts it had uploaded and keeps
the body. "Re-run all jobs" does nothing useful there, because release-please
finds its pull request already tagged and skips the build.

The workflow tags nothing until the merged release pull request's own commit
has a green `ci` run on `main`, so a release never goes public without its
artifacts. A run that finds that commit's suite still running, or red, holds
the release and says so in its log. The commit's run going green, by itself
or by a re-run of a flaky job, starts the run that cuts the release.
