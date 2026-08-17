# substrate console

The shadcn-native console for the substrate. The app only ever speaks
**same-origin** `/api/v1` — in production one host serves both the SPA
and the API; in dev the Vite proxy (`vite.config.ts`) forwards `/api` and
`/healthz` to a substrate.

## Local dev

```bash
pnpm install

# with no override the dev proxy targets a substrate on this box
pnpm dev

# point it somewhere else
VITE_PROXY_SUBSTRATE=https://substrate.example.com pnpm dev
```

`VITE_PROXY_SUBSTRATE` is the only knob — nothing in `src/` hardcodes a host.
Sign in on `/login` with a username, a password and the current TOTP code (the
door says at `GET /api` whether it wants the third: `mise run dev` runs with the
factor off, `mise run dev:totp` with it on). What comes back is a token RECORD
and its secret — there is no session beside it, so what the browser keeps in
localStorage is a `substrate_tok_…` like any other client's, and a 401 anywhere
drops it.

## Checks

```bash
pnpm typecheck   # tsc --noEmit
pnpm lint        # eslint
pnpm fmt:check   # prettier --check
pnpm test        # vitest run
pnpm build       # tsc -b && vite build
```

`mise run ci:console` is all five, and what CI runs.

## Pages

| Route                          | Surface                                                                                                            |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `/login`, `/register`          | the door; everything else needs a session                                                                          |
| `/`                            | overview                                                                                                           |
| `/changelog`                   | the repository's change feed                                                                                       |
| `/registry`, `/registry/:id`   | the catalog: install, upgrade, lifecycle verbs, input resolution + bind, accountconfig trait query + OAuth connect |
| `/agents`, `/agents/:id`       | declared agents + llm rows; the `:id` route is the ndjson streaming chat                                           |
| `/merge-requests/:id`          | a duplicate-suggestion verdict                                                                                     |
| `/data/:authority`             | one authority's collections                                                                                        |
| `/data/:authority/:kind`       | browse a kind's records                                                                                            |
| `/data/:authority/:kind/new`   | create a record                                                                                                    |
| `/data/:authority/:kind/:id`   | one record, and `/edit` beside it                                                                                  |
| `/actors/:id`                  | an actor's view                                                                                                    |
| `/account`, `/account/tokens`  | the account, and the tokens it has minted                                                                          |

`/account/tokens` is deliberately not `/tokens`: the API door answers
`GET /tokens`, so a browser refreshing that path would be handed JSON.

## Adding shadcn components

```bash
pnpm dlx shadcn@latest add <component>   # docs first: shadcn@latest docs <component>
```
