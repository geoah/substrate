<!-- The title is the commit, and the commit is the release. A squash merge
     lands this title as the commit and a rebase merge lands every commit on
     the branch, so both are held by the `conventional commits` check:
     `type(scope): what changed`, with `!` before the colon for a break. Types
     in use: feat, fix, docs, refactor, test, chore, ci. Merging a green
     `feat:` or `fix:` tags main and publishes it. -->

## What this changes

<!-- What moved, and why. The diff says what; this says why. -->

## How it was checked

<!-- Delete what does not apply. `mise run ci` is the whole pipeline. -->

- [ ] `mise run test` (or the suite that covers this: `test:db`, `test:race`, `console:test`)
- [ ] `mise run lint` and `mise run fmt:check`
- [ ] Ran it against a substrate (`mise run dev`)

## Things this repository will ask about

<!-- Tick what applies; ignore the rest. Each of these is a check that fails
     late and confusingly if it is missed. -->

- [ ] **A declaration under `kinds/` or `samples/` changed** — its version is
      bumped (the kind's own for a one-kind change, the package's for a
      closure-wide one or a removal). `mise run kinds:check` is the guard.
- [ ] **A Go wire struct changed** — `wire.golden.json` is regenerated and
      `types.ts` follows it (see [testing](../docs/testing.md#the-wire-drift-guard)).
- [ ] **The API surface changed** — it is additive, or the break is stated
      here and in the title with `!`.
- [ ] **Somebody using a substrate has to act** (a break, a deprecation, a
      new env var or flag): a note is added under `docs/changes/`
      ([format](../docs/changes/README.md)). A `!` without one fails the
      `conventional commits` check.
- [ ] **Docs affected** — the pages that describe this are updated. `docs/` is
      held to the code, not the other way round.
- [ ] **A new word** — it is in `docs/terms.md`, and no dead word came back.
