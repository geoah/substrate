# Wave 4 brief — wrap up the console (four parallel agents)

The console redesign (PR #648, `console/redesign`) is nearly done. The owner wants it wrapped up and ready to merge. Seven engine PRs the owner is merging are stacked on an integration branch, `console/wrapup`, and your worktree starts from it. Your job is the CONSOLE side of those engine features, plus the design review items still open. Substrate (Go/kinds) fixes you need are allowed on this branch; follow AGENTS.md strictly for them.

Scratchpad = `/private/tmp/claude-501/-Users-geoah-src-github-com-geoah-substrate/ecb153f3-6121-45bd-9577-eebedb8a34f1/scratchpad`.

Read first: your worktree's `CLAUDE.md`, `docs/console/design-guide.md` and `docs/console/elements.md` (the design system as built: USE these components and rules), `docs/console.md`, and the findings text `scratchpad/fable-review/review.txt` for any review id named below. Then "Run and look" in `scratchpad/PAGE-BRIEF.md`. The dev substrate on :8097 was restarted with the `console/wrapup` binary, so the new APIs below are live there. Do NOT restart it. Providers installed before the restart are still at their old versions; `substratectl install providers.substrate.reamde.dev/google` (with your repository's `ctl-<repo>.yaml`) upgrades one if you need its new labels. Use `substratectl` from `/Users/geoah/.t3/worktrees/substrate/wrapup/bin/substratectl`.

Owner rulings this wave: the sign-in field keeps the word "Repository" (E2 answered). aria-sort IS now in scope (#677). Search scope is now served by the engine (`filter.purposes`, PR #670).

| area | worktree | branch | vite port | dev repository |
|---|---|---|---|---|
| agents | ~/.t3/worktrees/substrate/w4-agents | console/w4-agents | 5351 | agents.localhost |
| data | ~/.t3/worktrees/substrate/w4-data | console/w4-data | 5352 | table.localhost (and nav.localhost) |
| history | ~/.t3/worktrees/substrate/w4-history | console/w4-history | 5353 | record.localhost |
| table | ~/.t3/worktrees/substrate/w4-table | console/w4-table | 5354 | table.localhost |

Deps are installed and `web/console/public/__shot.html` is in place. It is git-excluded; move it aside into the scratchpad while running `ci:console`, then put it back.

## The engine surface (already on your branch)
- **#671, allow outranks a named gate.** `substrate.reamde.dev/core/recordpatchpolicy` gains `overrides` (a reference to another recordpatchpolicy), valid only on `action: allow`. The selector must name exactly one agent, one kind and one op. `overrides` must name a live gate, else 422. Revoke by deleting the rule or setting `disabled: true`. See `internal/engine/policy.go`, decision 0109, `docs/changes/policy-allow-overrides-a-named-gate.md`.
- **#674, change run summaries.** `GET /api/v1/changes?runs=1` takes `before`, `generation`, `first` (counts runs) and the change filters; it refuses `watch`/`from`. It returns `ChangeRunPage {runs[], cursor?, head, generation}`, each run `{actor, kind, verb, count, records, recordId?, newestSeq, oldestSeq, newestTs, oldestTs}`. `ChangeRun`/`ChangeRunPage` are NOT yet pinned in `internal/substrate/wire_test.go` or `types.ts`: add them to the Go wire test, regenerate the golden (`go test ./internal/substrate/ -run TestWireGolden -update`), and mirror them in `types.ts`. Decision 0110.
- **#672, package declared by.** `substrate.reamde.dev/core/package` gains a managed `declaredBy` string (the actor that first declared the package), stamped by the engine only and absent on older packages. Decision 0111.
- **#673, adjust a change request.** `substrate.reamde.dev/core/recordpatchrequest` gains `adjustedDiff` (json), written by the owner in the accepting PATCH beside `decision: accepted`. It replaces `diff` whole: a proposed property it omits is not applied, and `diff` keeps what was proposed. Its own `ifVersion` is the accept's check. Patch/create requests only. Decision 0112.
- **#675, kind display label.** `KindInfo.label?: {singular, plural}` is on the wire, and shipped labels include Slack conversation → Channels and Google calendarseries → Repeating events. Check that `lib/kind-names.ts` displayName/displayPlural PREFER the declared label, and delete word-list entries the labels now cover. Decision 0113.
- **#676, renames pair.** `PropertyChange.renamedFrom?` on `values=1`; the console side of value-moves may already be done by that PR, so check. Decision 0114.
- **#670, search.** `filter.purposes` (primary|supporting|internal) on `GET /api/v1/records`, lists and the ranked `q=` read; `lib/api/records.ts` `search(…, {purposes})`. Decision 0115.

## Areas

**agents**
- **#671 "Always allow this".** Bring the button back on a suggested-change card when the request was gated. It writes an `allow` naming that gate in `overrides` (one agent, one kind, one op); it needs the confirmation "<Agent> will make <kind> <verb> changes without asking. You can take this back in the agent's panel." The agent panel lists the rules it wrote, each with Revoke (sets `disabled: true` or deletes).
- **#673 Review.** The change request page's Now / If applied grid becomes editable with the property sheet's editors (a draft, like the create page's SheetDraft). Apply writes `adjustedDiff` when anything was changed. The page and History show what was proposed vs what was applied.
- **#678.**
  - A grants editor: "Can see" / "Can change", picked by collection name (KindRef), writing the agent record's `permissions`.
  - An agent page `/agents/$id`, or a panel tab if that reads better, with recent runs/threads, failures, token use and cost, and the policies that apply.
  - A model-key dialog: "Add your OpenAI key" writes the `llm/provider` row's `apiKey` through the same secret write the record page uses, and is shown where an agent refuses for a missing key.
- **#680 "Ask an agent".** Add a collection picks an agent whose write grant covers `substrate.reamde.dev/core/kind` and SENDS the message; when none can, the dialog says so ("None of your agents can set up a collection yet. Start from a sample, or give an agent that permission.").
- Owns: `components/agent/*`, `pages/agents*.tsx`, `pages/change-request-detail.tsx`, `components/change-request.tsx`, `lib/agent-*.ts`, `lib/api/agents.ts`, `lib/api/transcript.ts`, and new agent routes in `router.tsx`.

**data**
- **#672 "Made by".** Collection cards, the sidebar hover card and All data say "Made by <agent>" (OriginMark already has the words) when the package's `declaredBy` is an agent actor. A package page (`/data/<authority>/<package>`) reads as the app: its collections, the tools it ships, and the agent that made it.
- **#675.** Kind labels drive display names everywhere (sidebar, headers, crumbs, ⌘K, pickers, History). Remove word-list entries the labels cover.
- **Search scope.** Now that the engine has `filter.purposes`, everyday ⌘K and the Search page send `purposes=primary` (plus supporting if that reads better on real data; check it). Technical mode adds "Include the substrate's own records" (all purposes). This replaces the "Later" note; do it in both places.
- **Review leftovers.**
  - X2: the technical sidebar shows the label first, with the raw name in faint mono beside it.
  - T4: provider collections hide bookkeeping columns by default (properties whose `writer` is not the owner, and `deprecated` ones), still reachable from Columns.
  - T5: drop the hover-only Open button, or make it the row's keyboard target, and give the tree chip words ("1 of 3 subtasks done").
  - R2: the record hover card uses declaration labels and resolved reference titles, with keys only in technical mode, and a copy button on its footer reference.
  - Delete the leftover `components/record-pill.tsx` shim, which has no importers.
  - Contrast: the destructive ink on its soft fill (≈3.95:1) and the yellow hue ink (≈3.5:1) must reach 4.5:1 in light mode, with a test like `src/index-css.test.ts`.
- Owns: `components/app-sidebar.tsx`, `components/command-menu.tsx`, `pages/search.tsx`, `lib/search.ts`, `lib/kind-names.ts`, `pages/home.tsx`, `components/home/*`, `pages/all-data.tsx`, `pages/authority.tsx`, `components/identity/*` (R2 and hover cards), `components/data-table/*` ONLY for the T4 default-visibility function (coordinate: the table agent owns the rest of data-table), `index.css`.

**history**
- **#674.** Home's Recent changes and History use `runs=1`, so a run's count is exact from one read. Delete the read-until-the-run-closes loop and the client-side run folding they replace, keeping a fallback for a server without `runs`. Pin the wire types as described above.
- **#676.** Confirm History and the record page's History read a rename as one move ("Label renamed to Display label"). Finish it if the PR left gaps.
- **Y3.** The History table view shows the date on the first row of each day, the kind column shows the display name with the full reference in a title, and the whole row is the expand target.
- **Q2.** Against a server without `count`, Home fires at most one probe per collection and shows "many" rather than a late number.
- **Y2 check.** The raw actor id sits after the sentence, in technical mode, everywhere a sentence names an actor.
- Owns: `pages/history.tsx`, `pages/changelog.tsx`, `pages/actor.tsx`, `components/changelog/*`, `lib/history.ts`, `lib/change-values.ts`, `hooks/use-history-feed.ts`, `lib/api/changes.ts`, `lib/api/records.ts` (count probe), and the Go wire test for ChangeRun.

**table**
- **#677 aria-sort.** Add `aria-sort` on the sorted column's header cell, kept in step with the URL sort.
- **#679 Saved views and Group by.**
  - Saved views are a named filter, sort, column set and nesting per collection, stored on the `substrate.reamde.dev/core/consolepreference` record: add a `views` property to that kind with a version bump, following AGENTS.md; kinds:check must pass. Use a tab strip above the grid (All + saved + "Save view"), with rename and delete through ConfirmDialog.
  - Group by an enum, a state or a single reference, with collapsible group heads showing counts and working with paging.
  - A favorite star in the collection's page header.
- **consolepreference `sidebarOpen`.** No console writes it any more (decision 0132): mark it `deprecated: true` in the same kind bump.
- **T6.** Make docs/console.md say what the paging code does (offset paging, the cursor only tells whether there is a next page).
- Owns: `pages/kind-browse*.tsx`, `components/data-table/*` (except data's T4 default-visibility function), `lib/filters.ts`, `lib/table-prefs.ts`, `lib/record-tree.ts`, `hooks/use-record-tree.ts`, `kinds/substrate.reamde.dev/core/consolepreference.yaml`, `lib/console-preferences.ts`, `hooks/use-console-preferences.ts`.

## Rules
- No push, no PR, no AI attribution lines.
- Conventional commit titles, one logical change per commit, and a failing test first for bugs.
- Delete dead code you replace.
- Stay in your ownership. Shared files (`lib/api/types.ts`, `wire.golden.json`, `router.tsx`, `docs/console.md`, `docs/console/*.md`) take additive edits only, and you list them.
- Update `docs/console.md` and `docs/console/elements.md` for what you add, briefly.
- Everyday copy uses the word list in the design guide; technical facts go behind `useTechnicalDetails()`.

Verify `mise run ci:console`, plus for Go/kind changes `mise run test:short`, `mise run kinds:check`, `mise run lint` (ignore the two known macOS `lint:go` sandbox fields) and `mise run fmt:check`. Run the engine DB suite only if you touch the engine, and ALONE.

Keep screenshot loops to 8 images or fewer.

## Report
Include: commits (hash + title); what changed per item; shared-file edits; screenshots (paths under `scratchpad/shots-w4-<area>/`); known gaps.
