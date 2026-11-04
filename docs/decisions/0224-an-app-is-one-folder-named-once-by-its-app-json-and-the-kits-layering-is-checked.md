---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0224. An app is one folder named once by its app.json, and the kit's layering is checked

## Context and Problem Statement

The Apple apps lived under `apps/apple/` by platform, and each app was named
in thirteen `case` statements across the release script, in a scopes file
and in the Xcode project. Tens of apps on two platforms would multiply both,
and nothing held which library may import which, so a portable library could
quietly take an Apple-only dependency and break Android. [The app kit
plan](../plans/app-kit.md) (§6, §7) has the layout and the rules.

## Considered Options

- One folder per app (`apps/<app>/` with its core package, `apple/`,
  `android/`, `package/`), named once in an `app.json` that every tool reads,
  and the kit's layering held by a linter.
- Folders by platform (`apps/apple/`, `apps/android/`), with each app's facts
  kept where each tool needs them.
- One folder per app, named in YAML rather than JSON.

## Decision Outcome

Chosen: one folder per app named once by its `app.json`, with the layering
checked by `mise run lint:apps`. Everything one app is sits together, so
adding an app is a folder and no edit to the release, the site or the server's
tests. `app.json` is the authority on an app's facts:
- These read it directly: the Gradle settings, the release and site tooling,
  and the server's first-party reach test.
- Its Swift manifest repeats some values, and a test holds them equal to it.
- The Xcode project includes each app's own `apple/targets.yml`, which
  `apps:new` writes.

So a copy is either generated or checked, never a second source. An icon of
the app's own is the one entry kept elsewhere, a line in
`apps/xcode/scripts/icons.mjs`. JSON over YAML because every reader parses it without a
dependency: Swift and Go tests, Gradle, `jq` in the release scripts and
Python. An app's core package is named for the app (`WorkoutsKit`,
`TasksKit`), because SwiftPM names a local package by its folder and one
Xcode project resolves every app's.

### Consequences

- Good, because a new app is `mise run apps:new <id>`. Beyond what that
  writes, only an icon of the app's own needs its name.
- Good, because a portable library or an app's core that imports a UI or an
  Apple-only framework, or a dependency pointing up the kit's order, fails
  `lint:apps` before it fails an Android build.
- Bad, because moving the apps touched the release script, the server's
  tests, goldens found by path and many documents at once.
- Bad, because `app.json` cannot carry comments, so what each field means
  lives in `apps/README.md`.

### Confirmation

`mise run lint:apps` (`.mise/appscheck.py`) holds package folder names
unique, the kit's dependency order, the portable import allow-list, no app
importing another, every app folder's `app.json` and its fields, and the
third-party package list. `mise run apps:new:check` scaffolds an app from
`apps/template` and builds and tests it, so the template cannot rot.

## More Information

On-device paths, bundle ids and Keychain groups did not move with the
folders, so installed apps upgrade in place. Worth reopening if an app ever
needs to ship from a repository of its own.
