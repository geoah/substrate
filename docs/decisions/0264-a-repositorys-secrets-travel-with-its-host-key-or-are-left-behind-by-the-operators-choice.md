---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0264. A repository's secrets travel with its host key, or are left behind by the operator's choice

## Context and Problem Statement

A repository's secrets (the login's password hash and TOTP seed, every
account's OAuth token, every secret property such as an API key) seal
under its data-encryption key, which was stored twice: wrapped under the
host credential key, and wrapped to an age recipient whose identity the
owner kept, in a `core/recoverykey` record. The second copy was read in one
place, the offline `repository rewrap` that let a directory boot on a host
without the original key
([0054](0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md)).
It could not be replaced once lost, and if it leaked it opened the secrets
of every copy of the data ever taken. The 1.0 plan (pull request 1313, F6
"Keys", row 55, step 8) removes it, which leaves an archive, an export or a
0.x directory brought to a host without its key with no way to restore.

## Considered Options

- Keep the recovery key and add a way to replace it.
- Remove it; a restore that cannot open a copy's secrets refuses, as
  before, and the operator who accepts the loss says so for that
  repository, by name or id, in `SUBSTRATE_IMPORT_WITHOUT_SECRETS`.
- Remove it; a restore that cannot open the secrets drops them on its own.

## Decision Outcome

Chosen: remove the recovery key, and restore without the secrets only for
a repository the operator lists. A copy whose wrap this host's key does not
open is refused, the refusal naming the key id the wrap names, the one this
host holds, the repository, and the variable. That holds on every door a
copy comes in by: the restore of an archive the database has no row of,
which is also how an export comes back, the staging of an archive the
operator takes in place of a repository, and the import of a 0.x directory.
Every refusal a restore makes of the copy's header (its name, its dialects,
its data key under this host's key or the listing, a host with no key, its
sealed values) is made by one admission. A take is admitted by the
restore's own reading of the copy staged for it, run to its end and
writing nothing (every history piece and its lines, the sealed values, the
spooled requests and the blobs, read by the code the restore reads them
with), together with that admission, before its choice is recorded and
again at the boot before anything of the repository goes, with the key and
the listing as they are then; a refusal leaves the repository whole and the
choice standing. A take reads its copy twice for it, and nothing lets the
second reading be skipped. After the removal, what can still fail the
restore is the database, a staged copy changed between the two readings,
and this binary's own conversion and fold of what it read; each leaves the
choice recorded and the archive it names in the store, so a boot after the
repair restores from it. The
server and the operator's commands that open as the writer read the listing
alike. A listed one is restored with a new data-encryption key under this host's
key; every record and every line of history arrives, and the open deletes
the webhook requests it had spooled that do not open. A directory from
before repository ids is given its id at the same boot, and the listing
reaches that step too. A sealed value that does not open names its ref and
the record holding it in the clear, so it is the record of its own loss,
and nothing removes it but the write that clears its ref: the restore keeps
it and marks the repository's row as it creates it, and the next open takes
each one's ref out of the record that holds it, row by row and at any depth,
raises an alert for each, and removes the value in the same transaction,
then lifts the mark; a value whose ref its record does not hold stays, with
the mark, until the GC's sweep takes it. The archive's next pass leaves the value out of its manifest because
the table no longer holds it. A crash anywhere repeats harmlessly, a value
that opens is never touched, and a second restore of the same archive goes
on where the first left off.
No declaration is asked, so after the restore no read, listing or use of a
secret-typed property returns or acts on a ref the store does not hold,
whatever state its package is in then or later and however its type is
spelled; the owner sees in the alerts what to reconnect and which keys to
enter again, and an account whose token it was reads `erroring` with the
reason. The
login credential's material is a secret too: the owner signs in again after
`substratectl user reset`, which the host key still allows. A listed
repository the key does open restores with its secrets.

What the recovery key protected can be obtained again by reconnecting and
re-entering; a replaceable key would keep the step at registration and add
rotation for that gain. Dropping the secrets without the operator's word
would read a host key that was merely not set as consent to lose them, and
the key id in the refusal is what tells the two apart.

The wrap leaves stored data too. The `core/recoverykey` record of an
existing repository is purged by repository migration 8, and changelog
conversion step 2 drops `sealedKey` from the history line that wrote it, so
no copy taken afterwards carries it. The kind is not retired: the boot
upgrade never prunes a declaration, and a retired name a repository still
declares would refuse every later upgrade of `core`. A repository from
before keeps it declared and empty.

This replaces 0054: there is no rewrap, and a repository moves to another
host with its host key, or without its secrets.

### Consequences

- Good, because a leaked or lost owner's key no longer exists to leak or
  lose, and registration has one step fewer.
- Good, because a copy taken after the conversion carries no second wrap.
- Bad, because moving hosts without the host key loses every connected
  account and every key, and the owner's password: the operator resets it
  and the owner reconnects.
- Bad, because a copy taken before still holds the wrap, and whoever holds
  the identity still opens that copy's secrets.
- Bad, because an older server refuses a repository the conversion touched;
  the way back is a copy taken before the upgrade.

### Confirmation

`TestRegistrationHasNoKeyStep` (internal/api) holds the door's answer and
refuses the old request field; `TestAnExportRestoresUnderAnotherHostKeyWithoutItsSecrets`
holds the refusal, the listed restore and the same-key restore over HTTP;
`TestALostSecretIsClearedAndItsAccountAsksToBeReconnected` (internal/engine)
holds the clearing; `TestTheKeptDirectoryLosesItsRecoveryWrap` and
`TestKeptDirectories` (internal/testenv) hold the conversion of the release
kept as v0.123.1, and `TestTheKeptDirectoryImportsUnderAnotherKeyWithoutItsSecrets`
its import under another host's key; `TestARestoreWhoseOpenNeverRanIsClearedByTheNextOpenUnlisted`
(internal/engine) holds that the mark, not the listing, is what the open
acts on; `TestATakeTheListingNoLongerAdmitsRemovesNothing` and
`TestATakeOnAHostWithNoKeyIsRefusedBeforeItIsRecorded` hold the admission on
a take, and `TestATakeWhoseStagedCopyChangedRemovesNothing` that it is the
restore's own reading of the staged copy; `TestALostSecretBelowAPropertysTopIsCleared` and
`TestALostSecretItsRecordDoesNotHoldStaysWithTheMark` the clearing at any
depth; and `TestAnOperatorCommandReadsTheListingAsTheServerDoes`
(cmd/substratectl) the listing on the operator's path.

## More Information

docs/operations.md says what the operator does to move a repository, and
docs/stored-format.md lists conversion step 2. The archive and its restore
are [0279](0279-the-archive-holds-against-a-written-contract.md).
