## Context

Repository: geoah/substrate (Go server `cmd/substrated`, React console `web/console`, CLI `cmd/substratectl`, Apple apps `apps/apple`). Read `AGENTS.md` (CLAUDE.md links to it) in full before you change anything: it is binding.

The design you implement part of is `docs/plans/authorities.md` on branch `origin/docs/authority-documents` (PR #1145). Read it with `git show origin/docs/authority-documents:docs/plans/authorities.md`. You implement one work package of its phase 1, below. Do not implement other phases or other work packages.

Foundation, PR #1146 (branch `origin/feat/domains-record`): `internal/domains` parses the one `v=substrate1` TXT record at `_substrate.<name>` (`Parse`, `Lookup`, `Static` for tests, keys `server`, `proof`, `doc`, `sha256`), and `authorize.ConfinedClient()` is the confined HTTP client (public addresses only, no proxy, no redirects) for any URL a request may name. Use both; do not write a second TXT parser or a second confined transport.

Other agents work in parallel on the sibling packages: verified apps (0209), repository domain verification (0208), the package creation rule (0210), the Apple apps. Stay inside your scope so the branches merge.

## Rules

- Never write an em dash or an en dash anywhere: code, comments, docs, commits, PR text. Use a comma, a colon, parentheses or two sentences. Write plain statements of fact; comments carry constraints, not narration.
- Tests: run only the tests for what you change, `go test -count=1 -skip '^TestLive' -run '^TestA$|^TestB$' ./internal/<pkg>/`. NEVER run `go test ./...`, `mise run test`, `mise run test:db*` or any full database suite: one full suite per machine, and the orchestrator runs it. Always check the exit code; never pipe a test command into `tail` without `set -o pipefail`.
- Do not run `mise run dev` or touch ports 8080/5433: they belong to other sessions. If you need a live server, run an isolated stack (a throwaway pgvector container on a free port, `bin/substrate` on a free port with its own data root and credential key) and remove it when done.
- Never kill a process you did not start. Never use `git stash`. Delete a branch only by its exact name.
- Keep `mise run lint` and `mise run fmt:check` green; when you touch `web/console`, also `mise run console:lint`, `mise run console:fmt:check` and `mise run console:test`.
- A wire change updates `internal/substrate`, regenerates `go test ./internal/substrate/ -run TestWireGolden -update`, and updates `web/console/src/lib/api/types.ts`. `internal/api` never imports `internal/engine`; an engine contract change also lands in `internal/api/fake_test.go`.
- A changed declaration under `kinds/` bumps its version (`mise run kinds:check`).
- Never leak a deployment: tests and docs use `example.com`, `example.org`, `example.net`. `substrate.reamde.dev` is fine (the public placeholder authority).
- Decision record: write `docs/decisions/<NNNN>-<slug>.md` with the number assigned to you, from `docs/decisions/template.md`, `status: proposed`, `date: 2026-11-02`, `decision-makers: George Antoniadis`; add it to `docs/decisions/README.md` like the others; then `git fetch --prune && mise run decisions:check`. Keep it short, and say which records it amends.
- Docs follow the writing rules in AGENTS.md and `docs/terms.md`; `mise run lint:docs` must pass.
- Commits and the PR title are conventional commits; a break carries `!` and a `BREAKING CHANGE:` footer with the literal upgrade steps and what a rollback does. Open the PR with `gh pr create` against the base named below. Body: the problem, the change, and the checks you actually ran, under 200 words plus the checks. Do not merge, do not watch the PR, never trigger CodeRabbit.
- Finish with a short report: what you built, what you could not do, decisions you made that the plan did not settle, and as the very last line: `PR <url>, head <sha>, checks and reviews <state>`.
