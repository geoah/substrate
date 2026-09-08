---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0054. A repository moves between host keys through an offline rewrap of its manifest

## Context and Problem Statement

A repository directory ([0051](0051-a-repository-directory-is-the-backup-unit.md),
[0052](0052-the-authority-is-the-repository-id.md)) carries its DEK wrapped
under `SUBSTRATE_CREDENTIAL_KEY` in `repository.json`, and the boot import
refuses a directory whose wrap the host's key does not open. The same DEK sits
in the changelog wrapped to the user's age recipient (the `recoverykey`
record), which is the recovery promise: the directory plus the identity is a
complete recovery. Nothing turned that promise into a restore
([#137](https://github.com/geoah/substrate/issues/137)), and
[0024](0024-the-credential-key-is-key-material-not-a-passphrase.md) records
that "nothing re-wraps a DEK under a new host key".

## Considered Options

- An offline operator command over the copied directory: unwrap the DEK with
  the identity, wrap it under the new host key, rewrite the manifest, then
  let the ordinary boot import run. No database.
- A server endpoint or an operator command through the DSN that takes the
  identity and imports the repository in one step.
- A boot-time prompt: the server, meeting a directory it cannot open, asks for
  an identity.

## Decision Outcome

Chosen: the offline command, `substratectl repository rewrap <directory>`
over `engine.RewrapRepositoryDir`. It walks the changelog files for the last
`recoverykey` write, opens its `sealedKey` with the identity, opens every file
under `sealed/` with the recovered DEK, wraps the DEK under the new key with
the same binding the engine uses (`dek\x00<authority>`) and rewrites
`repository.json`. The boot import is unchanged and stays the one restore
path.

It beats the other two because the identity never reaches a running server.
The identity is the one secret the substrate promised never to hold, and a
restore endpoint or a boot prompt would carry it through the process that
holds every other repository's DEK. It also needs nothing the directory does
not already have: no DSN, no row, no server, so it runs where the copy is.

Three rules go with it. The sealed files are the refusal gate: a manifest is
rewritten only after every file under `sealed/` opened under the recovered
DEK, because a manifest written over a DEK that does not open the store would
import a repository no login can open, which is exactly what the boot check
refuses. Nothing is synthesized: a directory with no manifest, or with no
`recoverykey` record, is refused rather than given a guessed username,
authority or key. And the identity is never an argument: a file or stdin,
and neither it nor the DEK is printed or logged.

This narrows 0024's "nothing re-wraps a DEK under a new host key" to the live
path: the wrap in a `repositories` row is still never re-keyed in place, and
host-key rotation for a running deployment
([#133](https://github.com/geoah/substrate/issues/133)) is still open. The
recovery path re-wraps one directory, offline, from the user's wrap.

### Consequences

- Good, because a lost host key is no longer a lost repository for a user who
  kept the recovery key: copy, rewrap, boot.
- Good, because the restore adds no code to the boot and no surface to the
  server; the import that every backup already takes is the only one.
- Bad, because the rewrap revokes nothing. A copy of the directory taken
  before it still opens under the old host key, and every historical
  `sealedKey` in the changelog still opens under the identity that wrote it
  ([#237](https://github.com/geoah/substrate/issues/237)).
- Bad, because a repository that never enrolled a recovery key has no wrap the
  command can open; its directory boots only under the key it was written
  with.
- Bad, because the operator holds the identity for the length of the command.
  The command reads it from a file or a pipe and keeps it in memory only, but
  the operator's shell is outside the substrate's control.

### Confirmation

`TestRewrapRestoresARepositoryUnderANewCredentialKey` (internal/engine)
registers with a client-minted identity, copies the directory, proves the
import refuses under a second key, rewraps, boots under that key, logs in,
reads the secret and the blob, and verifies. It also proves a wrong identity
is refused with the manifest unchanged.
`TestRewrapRefusesWithoutARecoveryKeyOrAManifest` holds the two refusals and
that no manifest is synthesized. `TestImportRefusesADirectoryTheKeyCannotOpen`
still holds the boot check for an unrewrapped directory.

## More Information

[Running a substrate](../operations.md#restore-without-the-credential-key)
has the procedure. Reopen when a live host-key rotation lands (#133): it may
want the same unwrap-wrap-write with the old host key as the unwrap source,
which this record neither adds nor rules out.
