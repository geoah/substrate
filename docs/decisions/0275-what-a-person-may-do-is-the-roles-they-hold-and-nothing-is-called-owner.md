---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0275. What a person may do is the roles they hold, and nothing is called owner

## Context and Problem Statement

Until identities ([0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)),
a repository had one person, who could do everything, and four checks in
the engine said "only the owner may". With several people in a repository,
some of them need less than everything. The 1.0 plan
([#1313](https://github.com/geoah/substrate/pull/1313), "Shared
repositories", "Roles") fixes the shape; this record is the choice made
from it, with what the plan left open.

## Considered Options

- Roles as records, each a list of rules of verbs over kind patterns, named
  by the person's actor, as Kubernetes does.
- A list of people, or of permissions, on each record.
- Keeping one owner and adding a read-only flag.

## Decision Outcome

Chosen: roles as records. A role is a record of `core/role`: a name and a
list of rules, each `{verbs, kinds}`, the verbs `read` and `write`, the
kinds in the pattern grammar an app's scope uses (`*`, `<authority>/*`,
`<authority>/<package>/*`, a kind, `implements(<trait>)`, `~`). Rules only
add. A person's actor names the roles they hold (`roles`); they are read on
every request, so a change holds from the next one. A record list per
record and an owner flag were both rejected by the plan: a repository is
the unit of privacy, and a kind the unit of permission.

- **Three roles ship**, written by the engine into every repository:
  `admin`, `editor` (reads and writes every kind that is not a control
  kind) and `viewer` (reads them).
- **`admin` may do everything.** It reaches every control kind and sign-in
  material by being `admin`, not by a rule naming them: the settings a
  package takes are kinds of each provider's own package, which no shipped
  rule could list. A rule naming a whole class of control kinds would close
  that gap differently, and is an open question for George; the rule's
  shape can grow a key for it without breaking a stored role.
- **A rule's pattern reaches a control kind or sign-in material only by
  naming that kind exactly** ([0276](0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)).
  Unlike an app's scope, a glob reaches a kind whose purpose is `internal`.
- **Who may do what is a fixed check, never a rule.** No rule writes an
  actor or a role, the roles an actor holds among them; changing them is
  the `admin`'s, and the last person holding `admin` keeps it. The plan says
  so: "Only an admin changes roles, and the roles an actor holds. No rule
  can hand that out."
- **Nothing is called owner.** The creator is the first admin, all admins
  are equal, and a repository from before roles gives its person `admin`
  (repository migration 13).
- **At 1.0 no door on the wire adds a person or changes a role.** The plan
  puts both after 1.0 in the clients. The operator's interface does both
  (`AddPerson` with roles, `SetPersonRoles`), and `core/role` and
  `core/actor` are written by the engine alone, like every system kind.

### Consequences

- Good, because a person with less than everything exists in a repository,
  and the shape that later adds people and custom roles is already stored.
- Good, because the four "only the owner may" checks are the one check's
  answer: an editor is refused a connection and another person's token for
  the same reason they are refused a trigger, and an adjusted change request
  for the reason they are refused its target.
- Bad, because `admin` is special in code, not only in its record: a role
  a repository writes later cannot say "everything" except by naming each
  control kind, until a class can be named.
- Bad, because nothing on the wire changes a role at 1.0, so giving a
  second person a role is an operator's job until the clients offer it.

### Confirmation

`TestEveryActorTypeAgainstEveryControlKind` and
`TestTheFourOwnersChecksAreTheOneCheck` (internal/engine),
`TestTheLastAdminKeepsTheRole`, and the HTTP tests in
`internal/api/roles_db_test.go`: a viewer, an editor, a role changed between
two requests.

## More Information

Amends [0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)
through [0295](0295-a-persons-token-is-held-to-that-persons-roles.md). The
plan's "Not offered": a list of people on each record, groups, rules that
deny, inheritance; and "Later": write only the records you created.
