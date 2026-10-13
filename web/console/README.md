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
Sign in on `/login` with a repository name, a password and the current TOTP
code (`GET /.well-known/substrate/server.json` says in `registration.totpRequired`
whether the door wants the third: `mise run dev` runs with the factor off,
`mise run dev:totp` with it on). What comes back is a token RECORD
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

## Pages

The live routes in `src/router.tsx`, each with the page it renders:

| Route                                      | Page                                                                            |
| ------------------------------------------ | ------------------------------------------------------------------------------- |
| `/login`, `/register`                      | the door; everything else needs a session                                       |
| `/`                                        | Home: what the repository holds and what just happened                          |
| `/data`                                    | All data: every collection, and Add a collection                                |
| `/data/$authority`                         | one authority's packages                                                        |
| `/data/$authority/$pkg`                    | one package: its collections and the tools it ships                             |
| `/data/$authority/$pkg/$name`              | browse a kind's records                                                         |
| `/data/$authority/$pkg/$name/new`          | create a record                                                                 |
| `/data/$authority/$pkg/$name/$id`          | one record, and `/edit` beside it                                               |
| `/agents`, `/agents/$id`                   | the chat over every agent; the `$id` route is one agent's page                  |
| `/tools`, `/tools/$authority/$pkg/$name`   | every function that can act on the data; the second route is one tool           |
| `/providers`, `/providers/$authority/$pkg` | every provider, and one provider's accounts, sync state and settings            |
| `/history`                                 | the changelog as sentences                                                      |
| `/search`                                  | the ranked read (`GET /api/v1/records?q=`) as a page                            |
| `/settings`                                | the console's settings: layout, the account, its tokens and the recovery export |
| `/merge-requests/$id`                      | a duplicate-suggestion verdict                                                  |
| `/change-requests/$id`                     | a proposed change, what accepting it would do, and its verdict                  |
| `/actors/$actorId`                         | an actor's view                                                                 |

Old addresses redirect: `/changelog` to `/history`; `/registry`,
`/registry/$id`, `/connections`, `/connections/$authority/$pkg/$name/$id` and
`/settings/$id` to `/providers` or one provider's page (a sample's
`/registry/$id` to `/data`); `/account` and `/account/tokens` to `/settings`.

## Adding shadcn components

```bash
pnpm dlx shadcn@latest add <component>   # docs first: shadcn@latest docs <component>
```
