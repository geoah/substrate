# The Linear fixtures — what they are, and what they do not cover

16 recordings, a CUT of the owner's real workspace, pseudonymised by
`tools/pseudonymise.py` (rules: `RULES["linear"]`) and repaired by
`tools/rawpull/linear.py --repair`. Nothing here is invented: every page is a
real GraphQL response with nodes removed, and every value in it is either the
API's own structure or a fake from the shared persona map.

```
python3 tools/rawpull/linear.py --days 30                  -> raw/linear   (gitignored)
python3 tools/rawpull/linear.py --cut /tmp/linear-cut      OFFLINE, by what the payloads CONTAIN
python3 tools/pseudonymise.py /tmp/linear-cut providers/linear/fixtures --provider linear
python3 tools/rawpull/linear.py --repair providers/linear/fixtures
python3 providers/linear/fixtures/audit.py                 must print 0
```

## The naming contract

`tools/mockserver.py` slugs a GraphQL POST by `operationName` ALONE — the
document text and the variables contribute nothing — so a recording is named
for the operation that asked for it and nothing else:
`POST_graphql__op-<Stage>Page<N>.json`. That is what makes a paged connection
replayable: without the page in the name, page two would overwrite page one.
It also means the issue WINDOW never decides which file answers, so the same
set replays under any `backfillDepth`.

## What the set holds

| | |
| --- | --- |
| 15 issues over two real pages | assigned and unassigned, in a project, in a cycle, with labels, with subscribers, with comments, with reactions, with a parent, completed, canceled, archived, estimated, due-dated, integration-created, milestoned, moved between teams (`previousIdentifiers`), created from a comment, and one whose comments came back CAPPED |
| 54 members, 22 teams, 74 workflow states, 14 labels, 12 projects, 18 cycles | the transitive CLOSURE of everything those issues point at, plus enough extras that every connection has a real second page |
| 5 project statuses, 1 workspace, 1 viewer | from `organization` and `viewer`, which are single objects and not connections |

The cut is closed over its own references ON PURPOSE: a workflow state names
its team, a label names its team and its creator, a team names its default
state and its active cycle, and a set that cannot resolve its own references
proves the opposite of what the e2e asserts.

## What it does NOT cover

A green run over this set proves nothing about any of these.

* **`botActor` and `delegate`** — no payload in a 1,000-issue pull carries
  either, so the two properties are declared, extracted and never exercised.
* **A rate limit.** Linear answers 429 with `Retry-After` and a
  `RATELIMITED` GraphQL error at HTTP 200; the body handles both and parks
  `retryNotBefore`, and NOTHING in this set makes it happen. The mock takes
  injected faults (`mockserver.set_faults`), so this is a scenario the next
  round can write rather than a missing recording.
* **A bounded drain that the RUNNER has to continue.** `PAGES_PER_RUN` is 8,
  so the 16 recordings take two invocations and the parked walk (`syncPending`:
  stage, page, cursor, floor) IS exercised — but both invocations belong to one
  drain, so the continuation the runner drives by stamping `syncRequestedAt`,
  and a walk that survives the engine dropping a checkpoint, are the SEED's to
  prove.
* **A second account.** One workspace, one key.
* **A gated feature.** `project.previousIdentifiers` answers HTTP 400 for the
  whole page while the workspace's `Project and initiative IDs` feature is
  off, which is why it is declared and not fetched — and why no recording can
  show what it looks like when it is on.
* **A capped `labels` or `subscribers` connection.** One issue's COMMENTS came
  back capped, and the scenario asserts both halves of what the sync does with
  it: the page's prefix IS written, because a comment is its own kind joined by
  `comment.issue` and a prefix is a true partial read of it, and the account's
  status names the connection it could not finish. No issue in the pull carries
  more than `NESTED_PAGE_SIZE` labels or subscribers, so the "a property is
  left alone rather than written short" half of the rule is asserted by
  construction and not by data.
* **An issue whose parent is outside the window.** There are two, deliberately:
  they are the one reference the guide admits may dangle, and the scenario
  reports them by count rather than failing on them.

## What moves, and what stays

| moves | stays |
| --- | --- |
| every member name, display name, address, initials, job title and profile URL | every timestamp, count, boolean, enum and colour |
| the workspace's name, url key and previous url keys | Linear's own hosts and path grammar |
| every team name, key and display name; every label, state, project status, cycle and project name | Linear's own icon set and standard emoji shortcodes |
| every issue title, description, comment body, project content and quoted text — REPLACED with generated prose, never truncated | the response shapes, key order and array order, byte for byte otherwise |
| every UUID — rows, cursors, `labelIds`, upload paths — through one shape-preserving map, so the identity GRAPH survives and two different upstream ids stay different | the page-cursor CHAIN: page one's `endCursor` is still what page two was asked for |
| every composed value — `identifier`, `url`, `branchName`, the profile and project URLs — RE-DERIVED from the fake parts by `--repair` | |

## The audit

`audit.py` is the gate: it scans every value, every key, every path and every
file NAME, and every one of them again percent-decoded and base64-decoded,
against 19,038 real literals (the persona map plus every identity-bearing value
in `raw/linear`) and 3,774 real prose spans. It also checks that every prose
string in the set is one this repository's own generator could have written,
and that every value Linear COMPOSES recomputes from the fixture's own fake
parts. Zero is the only acceptable result, and the first two passes were not
zero: a real title slug rode inside every `<url|label>` link in a comment
body, and a member who signed in through Slack carried a `slack-edge.com`
avatar URL whose path is that workspace's own upload hash.
