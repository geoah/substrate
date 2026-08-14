<!-- The title is the commit. This repository squash-merges, and release-please
     reads the merged title off main to decide the next version and write
     CHANGELOG.md, so a title it cannot parse is a release that does not
     happen. Conventional commits: `type(scope): what changed`, with `!` before
     the colon for a break. Types in use: feat, fix, docs, refactor, test,
     chore, ci. -->

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

- [ ] **A declaration under `kinds/` changed** — its version is bumped (the
      kind's own for a one-kind change, the authority's for a closure-wide one
      or a removal). `mise run kinds:check` is the guard.
- [ ] **A Go wire struct changed** — `wire.golden.json` is regenerated and
      `types.ts` follows it (see [testing](../docs/testing.md#the-wire-drift-guard)).
- [ ] **The API surface changed** — it is additive, or the break is stated
      here and in the title with `!`.
- [ ] **Docs affected** — the pages that describe this are updated. `docs/` is
      held to the code, not the other way round.
- [ ] **A new word** — it is in `docs/terms.md`, and no dead word came back.
