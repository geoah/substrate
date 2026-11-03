---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0215. Code an approval installs reaches no further than the scopes it approves

## Context and Problem Statement

[0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)
lets an app's approval install a package's code and said so as a
consequence: the owner consents to the catalog's code, which nothing
bounds. A function that reads every kind and declares network can send any
record anywhere, whatever scopes the app was given, and the page's
sentence "it will be able to" stops being true the moment a package lands.
Phase 2 brings packages a publisher's document serves, so the code an
approval installs is no longer the binary's
([the plan](../plans/authorities.md), decision 9;
[phases 2 and 3](../plans/authorities/phases-2-3.md)).

## Considered Options

- Unbounded and disclosed: the page lists the functions, and the owner
  consents to the catalog's code (0202 as it stood).
- Installed code reaches no further than the scopes the approval approves:
  a package whose code reads or writes past them is not installed.
- A permission grant of its own per function on the page.

## Decision Outcome

Chosen: installed code reaches no further than the approved scopes,
because it is the one option under which the page's sentences stay true
and there is one rule to check, at planning. The grant per function was
rejected because it is a page no owner reads. Unbounded was rejected
because a published package makes the catalog's reach the app's in
practice.

The rules this binds:

- **The reach of a closure** is every kind, or glob over kinds, its code
  touches once installed: each function's `permissions.reads.kinds` (read)
  and `permissions.writes` (write), each agent's `permissions.reads.kinds`
  and `permissions.writes`, each record trigger's `source.record.kinds`
  (read), and each mapping's `from` (read) and `to` (write). It is read in
  `internal/catalog` from the documents the install applies (`Bundle.Reach`),
  once per closure, bare names completed under the declaring package. An
  agent's tools add nothing: a host tool runs under the agent's own grants,
  and a bundle function it calls runs under that function's.
- **Every closure an approval installs or upgrades is held to the scopes
  left checked**, the requested package and every requirement the catalog
  installs first, the binary's and a publisher's alike
  (`internal/appsetup`, `Options.Approved`). A package in place is not
  held: its code runs already.
- **Coverage is the grant's own rule** (`scope.Approved`, over
  `scope.Covers`): a write scope covers a read; a glob scope covers the
  kinds it matches and a glob no wider; a kind whose declared purpose is
  `internal`, as the registry holds it or as a closure of the install
  declares it, is covered by its exact scope alone, and so is a kind nobody
  holds or declares; the auth kinds are covered by nothing, and a write of
  the `core` or `llm` package by `full` alone, since no scope writes them.
  `full` covers everything.
- **A row reaching further is blocked** `reaches-past-scopes`, on the
  package and on each requirement whose code does it, the problem naming
  each uncovered reach and the scope that would cover it (`full` where no
  scope can). A block sentence otherwise carries no id; this one does,
  because a reach has no other name. Every row carries its reach on the
  wire, each entry with what reaches it, whether it is covered and the
  covering scope, and each function its reads, writes and declared hosts.
- **The plan is made against what is checked.** The approval page's read
  takes the scopes left checked (`GET /api/v1/authorizations/{id}?scope=`,
  every one when absent, none when empty), the approval plans against the
  scopes it names, the set up plans again against them before it writes,
  and a token's set up check against the token's own scopes. Unchecking a
  scope on the page reads the request again, and Approve waits for that
  read, so the plan hash the approval names is the plan made with the
  scopes it approves.
- **An upgrade whose reach grows waits for the owner.**
  `catalog.ReachGrows(held, candidate)` is each entry of the candidate's
  reach no entry of the held closure's covers, by the same rule; the
  publisher refresh of decision 0213 treats any as an upgrade that changes
  code.

### Consequences

- Good, because an app's token holds at least what the code it installed
  reaches, so revoking the app or reading its scopes says what the owner's
  server runs for it, and the page's sentences stay true after the install.
- Good, because the rule is one function of the closure and the scopes,
  with no new grammar: a package that needs wide reads asks for them as
  scopes, which the owner sees.
- Bad, because an app that installs a package must ask for every kind its
  code reaches, which widens its token: Substrate Sync asks to write the
  applehealth package's kinds, the two internal ones by name, and the
  health and observations kinds its adapter writes, where it asked to
  write its own kinds and read two.
- Bad, because a package whose code writes a `core` or `llm` kind can be
  approved only with full access, and one reaching a kind that is not
  held and that no closure of the install declares only by that kind's
  exact scope.
- Bad, because a function's declared hosts are shown and not held: the
  sandbox lets a body that declares any network reach any public host
  ([0035](0035-a-network-body-connect-is-filtered-by-destination.md)),
  until outside code is held to its hosts
  ([#1144](https://github.com/geoah/substrate/issues/1144)).

### Confirmation

`internal/catalog`: `TestAClosuresReachIsEverythingItsCodeTouches`,
`TestReachGrowsOnlyPastWhatIsHeld`. `internal/scope`:
`TestAnApprovalCoversAReachAsAGrantCoversAScope`,
`TestTheCoveringScopeIsNamedForTheOwner`. `internal/appsetup`:
`TestInstalledCodeIsHeldToTheScopesLeftChecked`,
`TestAnAgentsAMappingsAndATriggersReachCount`. `internal/api`:
`TestUncheckingAScopeBlocksTheCodeItCoversAndCheckingItBackUnblocks`. The
console: `app-setup.test.ts` ("what a package's code reaches"),
`setup-row.reach.test.tsx` and, in `authorize.test.tsx`, "plans again with
the scopes left checked, blocks the code they stop covering, and approves
the plan read with them".

## More Information

Amends 0202: its consequence that the owner consents to the catalog's code
and not to the app's no longer holds, since the approval bounds what the
code reaches. Holding a function to the hosts it declares, and a server
setting that refuses outside code, are decision 0220's. Reopen when a
package needs a reach no scope grants (a write of `llm/memory` by an agent
that pins facts), which would need a scope of its own rather than full
access.
