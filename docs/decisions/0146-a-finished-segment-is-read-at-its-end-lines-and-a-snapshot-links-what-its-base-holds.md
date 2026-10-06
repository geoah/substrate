---
status: proposed
date: 2026-10-06
decision-makers: George Antoniadis
---

# 0146. A finished segment is read at its first and last lines before serving, and a snapshot links what its base holds

## Context and Problem Statement

An upgrade stops the server for a verified backup, then boots the new
binary. On a 23 GB repository the backup took 2 h on amd64 and 6 h 40 min on
a Pi, and the boot spent 6.5 min of its 7.5 hashing finished segments
([#761](https://github.com/geoah/substrate/issues/761),
[#825](https://github.com/geoah/substrate/issues/825)). `repository snapshot`
read every segment five times (the open's digest, the verify's line pass, its
table pass, the copy, the read-back) although a finished segment never
changes. At 50 GB no pass over every byte fits in a few minutes of
downtime, even at disk speed, so the downtime must not scale with the
history's size.

## Considered Options

- Make each pass faster: parallel segments, a line check without a decode
  (#747, #767). Already done; each pass is still the whole history.
- Leave it to the filesystem: ZFS, LVM or cloud-volume snapshots of the
  stopped data root and database. Seconds, and the right answer where it
  exists, but not every operator has one, and substrate cannot require it.
- Snapshot a running server's finished segments ahead of the stop, then the
  rest while stopped. Needs a second mode of the snapshot, and its own
  record of which segments a later run may skip.
- A base: the snapshot takes an earlier snapshot of the same repository,
  links every finished segment and blob the base holds unchanged, and
  checks and copies only the rest. The open and the boot read a finished
  segment's first and last lines and take the rest on its sidecar's word;
  the server digests the segments after the open.

## Decision Outcome

Chosen: the base, with the open and the boot reading only a finished
segment's first and last lines. `repository snapshot --base <earlier
snapshot root>` holds each finished segment of the source to the base's by
name, size and sidecar digest, from the two sidecars alone, for the run of segments from seq 1 that
end at or before the head the base's `snapshot.json` recorded. Each such
segment is a hard link to the base's file, and each `stored` blob the base's
`snapshot.json` lists with the declared size is too. The verify reads those
segments at their first and last lines and compares the table with the files
from the seq that ends the run. Everything else is verified, copied and read
back as without a base. A file the filesystem refuses to link is copied from
the source and read back. The open, the boot check and the operator's open
read each finished segment's first and last lines: each line holds its own
sum, the first is the seq the name says, the last ends a transaction, and
the contiguity check sees the seq it ends at. The last segment listed is
digested whole, finished or not, because no next segment holds its end. The server then digests those
segments one at a time behind the open; a segment that does not match its
sidecar latches the repository's writes refused (`ErrChangelogDamaged`, a
corrupt-data error). `repository verify` still reads every byte.

It beat the filesystem because it works on any POSIX filesystem with hard
links, and it beat the running-server pre-stage because a base is a finished
snapshot: no new state, no new file format, and a snapshot taken with a base
is a whole directory that outlives its base. Reads were never unguarded: a
line a read returns is held to its own sum, so a damaged byte in a finished
segment fails the read that meets it whether or not the open digested the
segment.

### Consequences

- Good, because the backup costs the bytes written since the base, and the
  boot costs the active segment and two lines per finished one, whatever the
  history's size.
- Good, because the snapshot's on-disk format, `snapshot.json` and the
  restore are unchanged.
- Bad, because a linked segment is the base's bytes, read when the base was
  taken and never since. Damage to the base after that is carried into the
  new snapshot unread, and found only when a restore's import reads every
  line. An extracted export carries `snapshot.json` too and can be a base,
  so the export now hashes every finished segment against its sidecar as it
  streams it; its bytes are not read back after extraction, as a
  snapshot's are.
- Bad, because with a base the snapshot compares the `changelog` table with
  the files only past what the base holds. `repository verify` beside the
  running server compares them all.
- Bad, because hard links share one inode across snapshots: damage to the
  file is damage to every snapshot that links it.
- Bad, because a server now serves, and for a while appends to, a
  repository whose finished segment is damaged between its first and last lines, until the
  digest behind the open reaches it. Before, the boot refused it.

### Confirmation

`TestSnapshotWithABaseLinksWhatTheBaseHolds`,
`TestSnapshotWithABaseCarriesTheBasesBytes`,
`TestSnapshotRefusesABaseThatIsNotASnapshot`,
`TestADamagedSegmentFoundAfterTheOpenRefusesWrites`,
`TestExportRefusesAFinishedSegmentThatDoesNotMatchItsSidecar` and
`TestTheServerDigestsEachSegmentOnceAfterTheOpen` in `internal/engine`, and
the `TrustSidecars`, `Known`, `SharedFinished` and `CopyChangelogFrom` tests
in `internal/changelogfile`.

## More Information

[0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)
is the snapshot this extends. Revisit if a filesystem without hard links
becomes a common deployment, or if snapshots need to be checked without a
scratch server.
