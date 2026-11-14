---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0287. A repository's name is an alias, and a former name holds for 90 days

## Context and Problem Statement

A repository's name was permanent: a typo at registration, a start on
`localhost`, a later domain of one's own and a household named after one
person had no way out. Since
[0261](0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)
the id keys storage, and since
[0259](0259-a-repositorys-own-kinds-live-in-the-local-namespace.md) a
repository's own kinds are `~`, so nothing stored binds to the name any more
(the 1.0 review, F2, proposal point 2). What a rename leaves behind is every
address someone else holds: a webhook URL, an app's stored name and scopes, a
saved CLI context, a DNS record.

## Considered Options

- The name changes at once and the old one is free at once.
- The old name keeps resolving, and stays unavailable to anyone else, for a
  fixed time, then lapses.
- The old name resolves for good.

## Decision Outcome

Chosen: the old name keeps resolving, and is held against every other
registration and rename, for 90 days from the rename, then stops resolving and
is free at the same moment. A name freed at once would hand the addresses
others hold to whoever registers it next; a name held for good would let one
repository collect names without end.

- **The door** is `POST /repository/rename`, outside the versioned API beside
  the delete, with the person and their current factors in the body and no
  token, open to an admin of the repository alone
  ([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)),
  and its new name completed as a registration's is
  ([0288](0288-a-short-name-is-completed-under-the-configured-public-host-and-a-local-name-is-for-development.md)).
  It answers the id, the name and each former name in force with its moment.
  It is in the `auth` feature.
- **Former names live in one table**, `repository_former_names`, keyed by the
  name, with the repository and the moment it was retired. One lookup turns a
  name into a repository, reading the names and the former names in force,
  and every door that takes a name goes through it: a test fails on a query or
  a comparison that does not.
- **A second rename** retires the name it replaces from its own moment and
  leaves earlier former names to theirs. **Renaming back** to a former name
  makes it the name again. **Deleting** the repository frees its name and its
  former names at once, as
  [0236](0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)
  frees the name.
- **A former name** is another spelling of `~` on every input and resolves on
  every door that takes a repository's name, a sign-in's choice of repository
  included, whose answer names the repository by its name now. A person's
  sign-in name is their identity's
  ([0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)),
  linked to the repositories by id, so a rename moves none. Its domain proof does not carry over: the
  verification of the old name is cleared, and the new name is proved anew
  ([0208](0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)).

### Consequences

- Good, because a rename moves no stored value: the record, its manifest and
  its domain proof are all that change, and every address held outside keeps
  working for 90 days.
- Good, because a repository named under `localhost` before 0288 can leave it.
- Bad, because a name is unavailable to everyone for 90 days after its owner
  gave it up, and a name one repository cycles through stays out of reach for
  as long.
- Bad, because an address by a former name stops working on a known date with
  no warning to whoever holds it; the rename's answer is the only place the
  date is said.
- Good, because a hand of the repository's own carries `~`
  ([0259](0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)), so
  a rename leaves its actor record as it was: one record for each hand,
  whatever the repository was called when it wrote.

### Confirmation

`TestAFormerNameHoldsNinetyDaysAndADeletedNameIsFreeAtOnce` (the hold, its
end, a second rename, renaming back, deletion),
`TestARenamedRepositoryAnswersUnderItsOldNameAndItsNew` (every address over
HTTP under both names) and `TestEveryNameLookupGoesThroughTheOneFunction`.

## More Information

Amends [0236](0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)
by freeing former names with the name. The plan is the 1.0 review, F2 and its
validation item 9.
