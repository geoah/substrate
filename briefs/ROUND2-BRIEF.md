# Round 2 brief — console redesign follow-ups (six parallel agents)

The redesign is on branch `console/redesign`, open as draft PR #648 (https://github.com/geoah/substrate/pull/648). The owner reviewed it against their real substrate and asked for follow-ups. Six agents now work in parallel, each in its own worktree and branch off `origin/console/redesign`:

| area | worktree | branch | vite port | dev repository |
|---|---|---|---|---|
| count | ~/.t3/worktrees/substrate/r2-count | console/r2-count | 5321 | table.localhost |
| history | ~/.t3/worktrees/substrate/r2-history | console/r2-history | 5322 | record.localhost |
| copy | ~/.t3/worktrees/substrate/r2-copy | console/r2-copy | 5323 | nav.localhost |
| record | ~/.t3/worktrees/substrate/r2-record | console/r2-record | 5324 | record.localhost |
| table | ~/.t3/worktrees/substrate/r2-table | console/r2-table | 5325 | table.localhost |
| shell | ~/.t3/worktrees/substrate/r2-shell | console/r2-shell | 5326 | agents.localhost / providers.localhost |

Read first: your worktree's CLAUDE.md (AGENTS.md); `/private/tmp/claude-501/-Users-geoah-src-github-com-geoah-substrate/ecb153f3-6121-45bd-9577-eebedb8a34f1/scratchpad/DESIGN-GUIDE.md`; and the "Run and look" + "Rules" sections of `.../scratchpad/PAGE-BRIEF.md` (dev substrate on :8097, `shot-vite.sh <repo-prefix> <port> <out> <path>`, the per-repository CLI configs `.../scratchpad/dev/ctl-<repo>.yaml`, `bin/substratectl` from `/Users/geoah/.t3/worktrees/substrate/console-redesign/bin/substratectl`). `public/__shot.html` is git-excluded; if `fmt:check` flags it, move it aside while running CI and put it back. Do NOT restart the shared :8097 substrate. An engine agent that needs its OWN server runs one from its worktree binary on a free port (8110+) against a fresh database in the existing container `substrate-dev-db-redesign` (127.0.0.1:5436, user/pass postgres; `createdb` a new database named after your area) with its own `SUBSTRATE_DATA_ROOT` under the scratchpad and its own credential key — never the :8097 database.

Rules: stay inside your ownership (below); no push; no PR; no AI attribution lines; conventional commit titles (`feat(...)`, `fix(...)`), one logical change per commit, tests with every change (a failing test first for bugs). Engine changes follow AGENTS.md strictly (wire golden via `go test ./internal/substrate/ -run TestWireGolden -update`, never hand-edited; changelog is the truth; landed migrations never edited; a decision record only when the bar in AGENTS.md is met — the next free number is **0105**, and two agents may both want one: count takes 0105, history takes 0106 if it needs one). Verify `mise run ci:console` and, for Go changes, `mise run test:short`, the relevant DB suites (`mise run test:db:engine` ALONE, never concurrently with another agent's engine suite — if Docker looks starved, wait and retry), `mise run lint`, `mise run fmt:check`, `mise run kinds:check`.

## Ownership
- **count**: server-side record count (internal/api, internal/engine list/count path, internal/substrate wire, docs/api.md) + the console's `lib/api/records.ts` count functions and their callers' fallback; and the engine template separator fix (internal/vocabulary/template.go + tests; the console's `cleanTitle` in lib may stay as a guard for older servers).
- **history**: before/after values for changes (engine changelog/changes read path, wire, docs/changelog.md/api.md) + the console's History page, the record page's History section (components/record/*history*, lib/history.ts, hooks/use-history-feed.ts, components/changelog/*).
- **copy**: kind declarations' descriptions and names under kinds/providers.substrate.reamde.dev/* and samples/* (content only + version bumps), and the console's `lib/kind-names.ts` word list / `lib/kind-copy.ts` if names need it.
- **record**: components/property-sheet/*, components/record/* EXCEPT the history section, pages/record.tsx, pages/record-editor.tsx, components/record/record-combobox.tsx + identity-picker.tsx.
- **table**: pages/kind-browse*.tsx, components/data-table/*, lib/filters.ts, lib/record-tree.ts, hooks/use-record-tree.ts, lib/grid-values.ts.
- **shell**: components/app-sidebar.tsx, components/app-shell.tsx, components/command-menu.tsx, pages/agents.tsx + components/agent/* (thread-list, agent panel), pages/provider*.tsx + components/providers/* + lib/providers.ts.
Shared files (components/identity/*, lib/definition.ts, lib/api/types.ts, wire.golden.json, index.css): additive changes only, listed in your report.

## Report
Commits (hash + title), what changed, shared-file edits, screenshots (paths under `.../scratchpad/shots-r2-<area>/`), known gaps.
