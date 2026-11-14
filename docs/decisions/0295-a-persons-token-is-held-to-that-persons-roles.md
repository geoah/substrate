---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0295. A person's token is held to that person's roles

## Context and Problem Statement

[0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)
gave every token without scopes full access to its repository, because the
repository had one person who could do everything. With roles
([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)),
a token acts for a person who may do less. The 1.0 plan
([#1313](https://github.com/geoah/substrate/pull/1313)) lists 0143 as
amended: "a person's token is held to that person's roles". Its sign-out
half is [0272](0272-signing-out-everywhere-ends-every-token-the-person-holds-and-every-stream-ends-with-its-token.md).

## Considered Options

- Hold every token to the roles of the person it acts for, read on each
  request.
- Copy the roles into the token when it is minted.

## Decision Outcome

Chosen: hold every token to its person's roles, read on each request. A
token without scopes still has the person's whole access, which is now what
their roles give: everything for an admin, and for anybody else what their
rules reach, through the person's view. An app's token and an assistant's
act for their person and are held to that person's roles too. Copying the
roles into the token would let a removed role live on in every token minted
before, which the plan rules out ("Roles are read on each request, not
copied into the token").

A stream outlives its request. The lease that ends a watch, an agent
stream, an export or a blob's bytes when its token dies reads the person's
roles again too, and a change ends the stream: a watch with no terminal
frame, as a drain ends it, so the client reconnects from its cursor onto a
view of the roles held now, and an export cut before its last entry. A
bearer webhook, where an app's token stands in for a trigger's key,
delivers only while its person holds a role, and its run acts for that
person and is held to their roles, read when it runs: the person rides the
spooled request and the delivery's line, so a restart between the two does
not drop them. A request a keyed or signed sender makes acts for nobody, as
before.

### Consequences

- Good, because a role changed or taken away holds from the next request
  of every token that already exists.
- Bad, because every request reads the person's roles: two small reads of
  the repository's actor and role records; an open stream reads them again
  each time the changelog moves.
- Bad, because a role change ends every stream the person's tokens hold
  open, a whole export included.

### Confirmation

`TestARoleChangeHoldsFromTheNextRequest`,
`TestAWatchEndsAtARoleChangeAndReopensNarrower`,
`TestAnExportIsCutAtItsPersonsDemotionAndAtTheirTokensRevocation` and
`TestABearerWebhookIsHeldToItsPersonsRoles` (internal/api).
