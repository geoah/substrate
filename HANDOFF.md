# Console redesign: handoff (2026-09-27)

Notes-only branch; not for merging. Session materials that lived in a temp dir.

## Branches
- `console/redesign`: PR #648 (draft). Pushed, CI was green at 9eafcdb1/dbfbc948.
- `console/wrapup`: everything since, NOT yet in #648: the design docs (docs/console/*), wave 4 console work (Always allow + rules, editable Review with adjustedDiff, agent page + grants editor + model key dialog, Ask an agent, Made by + app page, kind labels, search scope via filter.purposes, runs=1 on Home/History, saved views + Group by + star, aria-sort, contrast), and merges of console/redesign (with #708) and origin/main.
  - The LAST commit `8042d635` merges origin/main: conflicts resolved, our decision records renumbered (purpose 0133, count 0134, change values 0135, search/#670 0136; console records 0130-0132; rename 0114 kept). CHECKS HAVE NOT RUN on it.
- `console/w4-fix`: four Codex P2 fixes (fresh cached count, picked saved view, editable create heading on Review, empty-Apply judged on whole diff). Pushed; NOT yet merged into console/wrapup.

## Next steps
1. On console/wrapup: verify the main merge. Run `mise run test:short`, `kinds:check` (declaration versions must exceed main's), `fmt:check`, `frozen:check`, `decisions:check`, `lint` (ignore the 2 macOS lint:go sandbox fields), `ci:console`, `ci:console:test`, and `test:db:engine` alone. Fix what is red.
2. Merge console/w4-fix into console/wrapup; rerun ci:console + ci:console:test.
3. Codex review of console/wrapup vs origin/console/redesign; fix P1s.
4. Merge console/wrapup into console/redesign, push (updates PR #648). Still open: #670 (search, targets console/redesign) and #682 (changelog docs); when they land, merge again and renumber if decisions:check asks.
5. Tell the owner the PR is ready to leave draft.

## Owner rulings (keep)
Console purpose = see, navigate, control data/providers/agents/tools; no inbox. Everyday copy + one Technical details switch. Kinds fully qualified where identity matters. Plurals UI-only. Paper design; left-aligned docs, full-width tables. Prefs on core/consolepreference; sidebar open per browser. Sign-in field keeps "Repository". One PR (#648) with many commits. No AI attribution in commits/PRs.

## Known gaps
Google etag/resourceName still default columns (mirror props, no writer role). Made-by only tested (no agent-made package on dev). Review cannot add properties the suggestion lacked. Chat card titles a create from suggested (not edited) values.

## Files here
briefs/: design guide + each round's brief. review/: the second design review's findings (ids S1...Q2, §20, §21).
