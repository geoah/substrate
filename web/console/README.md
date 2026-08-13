# substrate console

The shadcn-native console for the substrate. The app only ever speaks
**same-origin** `/api/v1alpha1` — in production one host serves both the SPA
and the API; in dev the Vite proxy (`vite.config.ts`) forwards `/api` and
`/healthz` to a substrate. Read
`docs/substrate/research/2026-08-substrate-console/GUIDE.md` before touching UI —
it is the binding style contract (the "one table system", tokens, the rules).

## Local dev

```bash
pnpm install

# with no override the dev proxy targets a substrate on this box
pnpm dev

# point it somewhere else
VITE_PROXY_SUBSTRATE=https://substrate.example.com pnpm dev
```

`VITE_PROXY_SUBSTRATE` is the only knob — nothing in `src/` hardcodes a host.
Log in on `/login` with a `substrate_tok_…` bearer (the token lives in
localStorage; a 401 anywhere drops the session).

## Checks

```bash
pnpm typecheck   # tsc --noEmit
pnpm test        # vitest run
pnpm build       # tsc -b && vite build
pnpm lint        # eslint
```

## Pages

| Route                                     | Surface                                                                                                                                 |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `/` `/stream`                             | overview, live change feed                                                                                                              |
| `/connectors`, `/connectors/:id`          | connector health + sync runs                                                                                                            |
| `/bundles`, `/bundles/:id`                | installed bundles: lifecycle verbs (disable/enable/uninstall/purge), input resolution + bind, accountconfig trait query + OAuth connect |
| `/triggers`                               | trigger bindings, run ledger, parked failures, wake/replay/retry verbs                                                                  |
| `/agents`, `/agents/:id`                  | declared agents + llm rows; the `:id` route is the ndjson streaming chat                                                                |
| `/merge-requests`, `/merge-requests/:id`  | duplicate-suggestion queue + verdicts                                                                                                   |
| `/data/:group/:type[/:id]`, `/actors/:id` | generic entity browse / entity page / actor view                                                                                        |

## Adding shadcn components

```bash
pnpm dlx shadcn@latest add <component>   # docs first: shadcn@latest docs <component>
```
