# substrate console

The shadcn-native console for the substrate. The app only ever speaks
**same-origin** `/api/v1` — in production one host serves both the SPA
and the API; in dev the Vite proxy (`vite.config.ts`) forwards `/api`,
`/healthz` and `/.well-known` to a substrate.

## Local dev

```bash
pnpm install

# with no override the dev proxy targets a substrate on this box
pnpm dev

# point it somewhere else
VITE_PROXY_SUBSTRATE=https://substrate.example.com pnpm dev
```

`VITE_PROXY_SUBSTRATE` is the only knob — nothing in `src/` hardcodes a host.
Sign in on `/login` with a repository name and a password; the page asks for
the current TOTP code only when the door answers `totp_required`, which it
does for an account with a second factor (`mise run dev` runs with the factor
off, `mise run dev:totp` with it on). What comes back is a token RECORD
and its secret — there is no session beside it, so what the browser keeps in
localStorage is a `substrate_tok_…` like any other client's, and a 401 anywhere
drops it.

## Checks

```bash
pnpm typecheck   # tsc --build --force
pnpm lint        # eslint with type-checked rules and import layering, then knip
pnpm fmt:check   # prettier --check
pnpm test        # vitest run
pnpm build       # tsc -b && vite build
```

`mise run ci:console` runs the type-check, lint, format check and a Vite
build; `mise run ci:console:test` runs the tests. Both are what CI runs.

## A position the server can no longer resume

A list read page by page, an older page of History, a History run's rows and
the live tail each resume a position the server handed out. When the server
answers `410 compacted`, the console follows RULE W, one recovery a minute
by time alone (`src/lib/api/compacted.ts`). A refusal starts a recovery: what
was read from the refused position is dropped, and the read starts again
from its first page, or a watch opens again at the head. Another `compacted`
within 60,000 ms (`RECOVERY_ALLOWANCE_MS`) of the START of the last recovery
is the failure, shown as one, never a second recovery; one at 60,000 ms or
later recovers again. Nothing else makes the allowance whole: not a
bookmark, a list that lands, a delivered change or the reader mounting
again. Whether a refusal ends as a recovery or as the failure, the refused
position is never sent again.

The allowance belongs to the reader's identity, not to a mount: it is kept
in `compacted.ts` under the reader's key, with the rule's only clock
(`Date.now`, which tests step), so a reader mounted again within the minute
meets the allowance it left, with the same start. An entry a minute old is a
whole allowance and is pruned at every refusal, so the map holds only the
readers whose recovery started in the last minute. A session for another
repository forgets them all (`forgetRecoveries`).

A reader comes under the rule by registering a `ResumingReader`, and the
type requires all of it: its `key`, a `drop` of the position it saved and a
`fail` that shows the failure. `recoverOrFail` answers every refusal: it
calls `drop` first, the failure's included, then starts the recovery or
calls `fail`. The readers:

- A list read whole goes through `walkPages`, keyed by the list it walks,
  or `startOverWhenRefused` for the time seek and the page read below it.
  Its drop is the pages the refused run read, and the kept seek below a
  page; its failure is the read's error.
- A list paged with Load more is an infinite query, held by
  `restartRefusedWalks`, which `main.tsx` installs on the console's one
  client, keyed by its query key. Its drop empties the query, cancelling a
  read in flight. A recovery shows the list's loading state, then a fresh
  first page, and no error. The failure holds the refusal as the query's
  error with no pages (`holdAtRefusal`), so neither Load more, Try again nor
  a mount can send the refused cursor; each reads the first page.
- A watch is `watchChanges`, keyed by what it follows; a watch opened again
  for its recovery is the same reader, and its failure is `stopped`.
  `followChanges` opens such watches for a surface that re-reads through
  its own queries. History's tail drops its pages, which name the head and
  generation it resumed, and its failure holds them at the refusal: History
  says it didn't load, with Try again, and the tail `Stopped`. The
  changelog table drops its pages, the head and generation it kept and a
  kept time seek, and says the same. The Agents chat list's watch resumes a
  read its caller caches, so its drop records that read (`dropPosition`) and
  a later stream opens at the head instead. The unit graph's watch, a
  page's live re-reads and an applet screen's watch open at the head and
  save nothing (`nothingSaved`). An applet says its failure over the screen
  still drawn; a reader whose surface has no status line yet (Home's recent
  changes, the collection and record pages, the Agents pages: issue #1378)
  keeps its failure where the shell can read it (`reportUnshown`,
  `unshownFailures`), which nothing renders yet.
- A History run's rows are read once from the position their summary named,
  under the feed's allowance, keyed by the feed's filter
  (`useHistoryFeed`'s `runRecovery`). Refused, that position is dropped and
  never sent again, and the summary is read again; the failure is said on
  the run's row, beneath its sentence.

`recordsQueryOptions` takes `FirstPageParams`, which refuse an `after`, so a
cached query never holds a cursor. `src/lib/api/compacted.source.test.ts`
reads the console's source and holds every place that can send a
continuation (`listPath`, `fetchRecordsPage`, `fetchChangesPage`,
`fetchHistoryPage`, `watchChanges`, `followChanges`, the time seek and every
`getNextPageParam`) to its table of readers, by file and function, each use
classified by the mechanism that holds it or as a first page, and checked
against where it sits; a watch is held only where it registers its reader. It
finds a sender under the name it was imported as (`as`) and as a namespace
import's member, refuses one reached by a computed name or exported under
another, and counts a parameter whose name is computed as a continuation;
fixtures run through the same scan show each. Any other refusal of a
continuation (a `422`, a `404`) stays what it is.

## Pages

The live routes in `src/router.tsx`, each with the page it renders:

| Route                                      | Page                                                                                                  |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------- |
| `/login`, `/register`                      | the door; everything else needs a session                                                             |
| `/authorize/$request`                      | an app's request for a scoped token: approve some of its scopes for a lifetime, or deny it            |
| `/`                                        | Home: what the repository holds and what just happened                                                |
| `/data`                                    | All data: every collection, and Add a collection                                                      |
| `/data/$authority`                         | one authority's packages                                                                              |
| `/data/$authority/$pkg`                    | one package: its collections and the tools it ships                                                   |
| `/data/$authority/$pkg/$name`              | browse a kind's records                                                                               |
| `/data/$authority/$pkg/$name/new`          | create a record                                                                                       |
| `/data/$authority/$pkg/$name/$id`          | one record, and `/edit` beside it                                                                     |
| `/agents`, `/agents/$id`                   | the chat over every agent; the `$id` route is one agent's page                                        |
| `/tools`, `/tools/$authority/$pkg/$name`   | every function that can act on the data; the second route is one tool                                 |
| `/providers`, `/providers/$authority/$pkg` | every provider, and one provider's accounts, sync state and settings                                  |
| `/history`                                 | the changelog as sentences                                                                            |
| `/search`                                  | the ranked read (`GET /api/v1/records?q=`) as a page                                                  |
| `/observations`                            | one row per stream in the records grid, and a Timeline panel drawing the selected streams             |
| `/observations/timeline`                   | the chosen metrics over a range, overlaid in charts of up to two units, one aggregate read per metric |
| `/settings`                                | the console's settings: layout, the account, its tokens and the recovery export                       |
| `/merge-requests/$id`                      | a duplicate-suggestion verdict                                                                        |
| `/change-requests/$id`                     | a proposed change, what accepting it would do, and its verdict                                        |
| `/actors/$actorId`                         | an actor's view                                                                                       |

Old addresses redirect: `/changelog` to `/history`; `/registry`,
`/registry/$id`, `/connections`, `/connections/$authority/$pkg/$name/$id` and
`/settings/$id` to `/providers` or one provider's page (a sample's
`/registry/$id` to `/data`); `/account` and `/account/tokens` to `/settings`.

## Adding shadcn components

```bash
pnpm dlx shadcn@latest add <component>   # docs first: shadcn@latest docs <component>
```
