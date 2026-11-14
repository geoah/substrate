---
status: accepted
date: 2026-11-06
decision-makers: George Antoniadis
amended-by: 0287
---

# 0236. An owner may delete their repository, and the name is freed

## Context and Problem Statement

Registration was one-shot and there was no way back: a repository, once
made, stayed until an operator wiped the database. Testing the door against
a deployed substrate therefore cost a fresh name every time, and an owner
who wanted out had nobody to ask but the operator.
[0046](0046-a-repository-owns-one-authority-chosen-at-registration.md) and
[0052](0052-the-authority-is-the-repository-id.md) call the authority
permanent, meaning nothing renames it. They do not say whether it can end.

## Considered Options

- No deletion; the operator wipes by hand.
- Delete the repository and reserve its name for good.
- Delete the repository and free its name.

## Decision Outcome

Chosen: delete the repository and free its name. An owner sends both current
factors to `POST /repository/delete`, as the credential changes take them, and
the engine erases every scoped row, the control-plane row and the repository
directory, blobs and spool included (`DeleteRepository` in
`internal/engine/auth.go`). It holds the authority's registration lock and the
open slot for the duration, so no registration of the name and no open of the
repository can start on rows being deleted. Afterwards the name registers like
any other. Reserving the name would have kept a tombstone row that every
lookup and the boot check would need to understand, for a risk that only
matters once other servers hold references to a name, which nothing on a
substrate does yet.

### Consequences

- Good, because tests can register, delete and register the same name again.
- Good, because an owner can leave without the operator's help.
- Good, because the erase reuses the failed-creation path, which already
  verifies that nothing survives.
- Bad, because the deletion cannot be undone: the only copy left is whatever
  backup or export the owner or the operator took before.
- Bad, because a freed name can go to somebody else, and a kind reference or a
  `_substrate` record somebody saved under it then points at a stranger's
  repository.
- Bad, because a leaked password and code together now destroy the data, not
  just the account.

### Confirmation

`TestDeleteRepositoryErasesItAndFreesTheName` (engine) and
`TestDeleteRepositoryTakesBothFactorsAndFreesTheName` (API), and the API's
bearer-refusal test, which includes the route.

## More Information

Revisit when an authority is referenced from outside its own server, for
example by federation or by apps verified against it, because then a freed
name is a takeover and a reservation may be worth its cost.
