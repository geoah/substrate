---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0282. A kind declares how long its history is kept, and a repository may override it

## Context and Problem Statement

Every entry of the changelog is kept forever, and a kind has nowhere to say
otherwise: the keys a declaration may carry are a closed set with no such
key. The 1.0 plan (pull request 1313, F6, rows 6 and 8, step 22) makes the
log compactable, the latest state of every record always kept and older
entries kept as a retention setting says, and this step lands the setting:
a kind declares everything, a length of time or nothing; a repository can
override it; the defaults are everything for kinds a person writes, 30 days
for mirrors of outside services and 7 days for bookkeeping. The compaction
that acts on it is a later step. The page leaves open the grammar, how a
shipped kind falls in a class, where an override lives and where the value
is read.

## Considered Options

- Grammar: one string, `all`, `none` or an ISO 8601 duration; or an object
  with a mode and a length.
- Defaults: the key on every shipped kind; a default on the package header
  that a kind's key overrides; or a class derived in code from a package's
  provider status or a kind's `purpose`.
- Override: a `core/retention` record per kind; a map on one settings
  record; a key on a package or on everything.
- Reading it: a computed field on the kind's record; a read route.

## Decision Outcome

**The grammar is one string**: `all` (every entry), `none` (nothing past
the latest state) or an ISO 8601 duration in the one grammar the vocabulary
has (`P30D`; no years or months, and no zero, which is `none`). Three cases,
three spellings, one value. Absent reads as `all`, so a declaration that
says nothing keeps what every kind kept before, and nothing is written into
it.

**A package header may state it for its kinds, and a kind's own key
wins.** The shipped classes are declared, not derived: a kind a person
writes states nothing; the ten provider packages state `P30D` in their
headers for the kinds that mirror their outside service, and each kind of
theirs the owner writes (a `config`, an `account`, Apple's `share`) states
`all` against it; the bookkeeping kinds, `triggerrun` and `alert` (0283),
a sync's progress through one item and Apple Health's delivery receipt,
state `P7D`. A class derived in code would
give a kind somebody else declares a value its author never stated, and
`purpose: internal` covers tokens, declarations and settings, whose history
is not bookkeeping. A kind somebody else declares with no key, and no key on
its header, keeps everything. `kinds/retention.golden.json` holds every
shipped kind's value, and a test refuses a kind it does not list until its
author says which class it is in.

**A repository overrides one kind with a record** of the new seeded kind
`substrate.reamde.dev/core/retention`: `appliesTo`, a reference to the
kind's declaration, and `retention`, the same grammar. A record is how this
tree keeps what an owner decides, and a shipped declaration is not the
owner's to edit. The write path admits one live record per kind and refuses
a malformed value; deleting the record restores the declared value. It
overrides a kind and nothing wider: a package's or everything's override is
an addition for later. Its writers are whoever may write an internal core
kind today. An override of a kind the repository does not declare applies
to nothing: it waits for the kind's declaration, and a kind removed keeps
all of its history. A kind move on the shipped upgrade carries the override
to the kind its rows moved to, as ordinary record writes; where that kind
holds one of its own, the one that keeps longer stands and the other is
deleted.

**An upgrade a person takes never shortens what a kind keeps unasked; the
binary's own upgrade applies the shipped defaults, and its release says
so.** Two retentions compare by how long each keeps, the effective one (the
override, else the kind's key, else its header's, else all) on either side,
so a respelling is no change. An upgrade that keeps any kind's history for
less time is not a safe addition. A published package's automatic refresh
leaves it for the owner. Every upgrade preview names each such kind
(`retentionShortens`, the `bundles` feature at revision 3). The add route
and the vocabulary apply take no consent to it, by design; the door a person
takes the upgrade through asks. The console opens its review for any update
whose preview names one, whoever keeps the package, so a provider or a
sample the binary ships is asked about as a published one is;
`substratectl add` refuses it until `--allow-shorter-history` takes it; an
app's approval sends the person to the package's page; and MCP's `packages`
`add` refuses it, telling the agent to send the person there. The seeded packages are the
exception: they upgrade with the binary at its boot, which has nobody to
ask. A release whose seeded packages keep a kind for less time says so in
its breaking-change footer, with the override that keeps the history. In this release that is
`core/triggerrun` and `core/alert`, from all to seven days. An override
wins over the declaration, so one written before the upgrade or after it
keeps what the owner chose. The day the compaction exists, a default
nobody was asked about or told of would otherwise erase history.

**The effective value is read in one place.** The engine's `retentionOf`
answers override, else declared, else all, and is what the compaction asks.
Clients read `GET /api/v1/retention` (the `retention` feature, experimental):
each kind's declared, override and effective value. `substratectl
retention` and the console's kind definition show it. A kind's record shows
only what its declaration states, as it does for `purpose`.

The keys are reserved by name (0020): `typeDataKeys` and `packageDataKeys`
gain `retention`, and the vocabulary dialect goes to 3, so an older binary
refuses a repository that holds one rather than quarantine its packages.

### Consequences

- Good, because a kind can say how much of its history matters, and an owner
  can disagree for their own repository without editing a declaration.
- Good, because the compaction has one function to ask and one value per
  kind, decided before it exists.
- Bad, because nothing acts on the setting yet: until the compaction lands,
  every entry is kept whatever it says.
- Bad, because an override covers one kind; keeping a whole provider's
  history longer is one record per kind.
- Bad, because an older server refuses a repository the new one opened (the
  dialect); the way back is the copy taken before the upgrade.

### Confirmation

`TestRetentionIsTheKindsElseThePackagesElseAll` and
`TestAMalformedRetentionIsRefusedWithAReason` (internal/vocabulary) hold the
grammar, and `TestARetentionComparesByWhatItKeeps` the comparison;
`TestAnUpgradeShortensWhatAKindKeepsByItsEffectiveRetention` (internal/engine)
and `TestARefreshLeavesAShorterRetentionToTheOwner` (internal/catalog) the
upgrade rule, the console's provider-page tests the review an update opens
for a shortening whoever keeps the package, `TestAddAsksBeforeKeepingLessHistory`
(substratectl) the command's refusal,
`TestPackagesAddPreviewsAndRefusesAShorterHistory` (internal/mcp) MCP's,
`TestAnUpgradeTheProviderPageMustReviewIsBlocked` (internal/appsetup) the
approval's, and
`TestTheBootUpgradeShortensTriggerRunsAndAlertsAndAnOverrideKeepsThemAll`
the binary's own upgrade and the override that keeps the history; `TestAKindMoveCarriesTheOwnersRetention` the move and
`TestAnOverrideOfAnUndeclaredKindAppliesToNothing` the undeclared kind; `TestEveryShippedKindsRetentionIsHeld` (kinds) holds the defaults
against the golden; `TestARepositoryOverridesAKindsRetention` (internal/api)
holds the declared forms, the override and its removal over HTTP; the
apply door, the catalog and the boot upgrade refusals are held there and in
`TestAMalformedRetentionIsRefusedAtEveryDoor`.

## More Information

docs/vocabulary.md, "The reserved keys", describes the key; docs/api.md,
"Retention", the route.
