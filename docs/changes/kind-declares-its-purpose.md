---
type: breaking
---

# A kind declares its purpose, and an older binary cannot read one that does

A kind may now declare `purpose: primary | supporting | internal`: why it
exists, so a client can decide what its navigation lists. Absent reads as
primary, and the server acts on nothing but the value's validity
(decision record 0106). The console's sidebar lists primary kinds and puts
the rest behind Technical details. Core's `kind` kind declares the key
(version 19), and the shipped providers and samples now carry it, so their
package versions were bumped.

A declaration's key set is closed. A binary from before this release
quarantines a package whose stored declarations carry `purpose`, and refuses
to open a repository whose core does. Once a repository has booted under
this release, going back to an older binary is not possible.

## What to do

1. Operators: upgrade every server that opens a repository before any of
   them boots this release, and do not roll back past it afterwards.
2. Authors: add `purpose: supporting` to a kind a person would not browse
   as its own collection (a sync state, a cursor, a join row), and
   `purpose: internal` to machinery. Leave the key off a kind that is a
   thing a person keeps.
3. Accept the upgrades offered for shipped providers and samples.
