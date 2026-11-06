---
status: proposed
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0232. Core maps the providers' people onto contacts, and a seeded mapping waits instead of refusing

## Context and Problem Statement

`substrate.reamde.dev/core/contact`
([0231](0231-a-contact-is-a-core-kind-of-the-owners-data-and-a-setting-names-the-owners-own.md))
is filled by nothing until something maps the providers' people onto it
([plan](../plans/contacts.md#what-fills-it)). Only the package that owns a
mapping's target may declare it
([0049](0049-the-owner-of-a-mappings-target-declares-it.md)), so the mappings
are core's. But a mapping whose source kind is absent refuses the registry,
core is seeded before any provider is installed, and no token may edit core:
a core mapping that refused would refuse every repository's own vocabulary,
with no edit that fixes it.

## Considered Options

- Core declares the mappings, and a seeded mapping waits while it cannot stand
- The same, but it waits only while its provider is absent, the rule the apply
  door's `holdWaitingMappings` uses
  ([0106](0106-the-apply-door-holds-back-a-mapping-whose-provider-is-absent.md))
- Each provider declares its own mapping onto `core/contact`, amending 0049
  and the loader check

## Decision Outcome

Chosen: core declares the mappings, and a seeded mapping waits while it cannot
stand. The nine mappings are in
`kinds/substrate.reamde.dev/core/contactmappings.yaml`, each filling a slot
named `contact` on its mirror. A mapping of `substrate.reamde.dev/core` waits,
left out of `Registry.Mappings()` so nothing projects through it, gives its
source a slot or counts it in an invariant, while any of these holds
(`internal/vocabulary/seededmapping.go`):

- its source kind is not installed;
- it does not resolve against the source as installed, as with an older
  provider version that lacks a path it reads;
- another package's mapping fills its slot, or its source declares the slot's
  name as something else;
- another package's mapping reads its target or writes its source.

The answer is recomputed at every registry build and stored nowhere. So the
install that brings a provider is the apply that switches its mapping on and
links the mirrors it finds
([0107](0107-an-apply-links-the-sources-its-mappings-left-unlinked.md)), and
an uninstall switches it off where a package's own mapping would refuse it
(0049 item 4). The boot upgrade that ships a mapping whose provider is already
installed links that provider's mirrors in its transaction, as the apply
would.

Waiting only on an absent provider was rejected because core has no owner to
fix the other cases. A repository may hold an older provider than the binary's
core expects, or an owner's mapping on the same slot, and 0106's reason for
refusing those (the caller can edit the mapping) does not apply to a mapping
nobody can edit. Each provider declaring its own mapping was rejected because
it reverses 0049 for one kind and still needs the same wait in every
provider, since `core/contact` is always present and the provider is not the
target's owner.

### Consequences

- Good, because every repository gets contacts from the providers it has
  installed, with no import and no per-provider vocabulary.
- Good, because a sample's or app's own mapping of the same mirror keeps
  working beside core's on another slot, and one on the slot `contact` takes
  it from core.
- Bad, because a mapping that does not fit waits silently to the API. The open
  logs one line per mapping that waits although its provider is installed, and
  `Registry.HeldMappings` is the only other reader.
- Bad, because the first open under the binary that ships the mappings links
  every mirror of every installed provider inside the boot upgrade's
  transaction, while every request to the repository waits. On a repository
  with years of mail that is one probe per address seen.
- Bad, because once any provider is installed, a client can no longer create a
  contact under an id it chose: a mapped kind's ids are server-assigned.
- Bad, because a mapping writes neither a constant nor a state. A contact
  minted from a mail address, a Slack bot or a Google label has no
  `contactType` and stays `utility` until a function or the owner sets them.

### Confirmation

`internal/vocabulary/seededmapping_test.go` holds the four conditions, the
install that switches a mapping on and the removal that switches it off, and
that another package's mapping still refuses without its provider.
`TestShippedBundlesInstallOnTheSeed` (`kinds/`) holds that every core mapping
waits on the seed alone and is live with every shipped provider installed.
`internal/engine/contactmapping_db_test.go` holds the install, the mint, the
link, that the owner's own card survives the recompute unmarked, and the boot upgrade
that links mirrors synced before the mapping.

## More Information

Step 2 of [docs/plans/contacts.md](../plans/contacts.md). Reopen when a second
seeded package needs mappings onto kinds it owns, or when one boot's backfill
is too large for one transaction (0107's paged job).
