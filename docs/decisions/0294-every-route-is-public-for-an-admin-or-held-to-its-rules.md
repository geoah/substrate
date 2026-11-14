---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0294. Every route is public, for an admin, or held to its rules

## Context and Problem Statement

With roles ([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)),
a person who is not an admin reaches the API. Before them, a route was the
owner's unless an app's token was let in, and a new route was the owner's
by default ([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).
The 1.0 plan's validation item 7 asks for a list of every route as public,
for an admin only, or limited by rules, where a new route fails until it is
placed.

## Considered Options

- Three doors in the router, and a list of every route a test holds to
  them.
- A check per handler.

## Decision Outcome

Chosen: three doors and a list. A route takes a token through one of three
middlewares: `requireAuth` admits a token whose person holds `admin` and
refuses an app's and a person held to their roles (`403 forbidden`,
`not_permitted`); `requireAuthAllowingApps` admits every token, each held to
its rules by the scoped view, an app's to its scopes and a person's to their
roles; `requirePerson` admits a person's own token for what is theirs alone,
their sign-ins and their identity. A route behind none takes no token, or
authenticates on its own path (the MCP door, the webhook door, the sign-in
doors). A route added later is an admin's by default. The list
(`internal/api/access_test.go`) places every served route, and
`TestEveryRouteIsPlacedInTheRouteList` reads each route's door off the
router and fails on a route the list does not place or places elsewhere. A
check per handler would let a new handler forget it, and nothing would
fail.

Held to rules, a person's reads are their rules', and no route answers
them more by being an app's door too: the list of kind declarations, the
console's and `substratectl kinds`' first read, holds the declarations of
the kinds their roles read, never every declaration, and a search of the
history reaches the kinds they read whole, where an app's token searches
none. A write their roles refuse says so: `403 forbidden` with the reason
`not_permitted`, where an app's scope refusal keeps its own answer.

### Consequences

- Good, because nothing a person who is not an admin asks reaches the
  engine except through the scoped view or a door that is theirs.
- Bad, because a route that should be held to rules has to be put behind
  `requireAuthAllowingApps` and spelled out on the scoped view, where it
  used to be an owner's route with no more thought.

### Confirmation

`TestEveryRouteIsPlacedInTheRouteList`, `TestAViewerListsTheKindsTheyRead`,
`TestAPersonSearchesTheHistoryOfWhatTheyReadWhole` and
`TestAViewersRefusedWriteSaysNotPermitted` (internal/api).
