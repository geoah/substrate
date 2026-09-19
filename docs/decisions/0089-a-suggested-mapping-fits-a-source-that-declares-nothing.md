---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0089. A suggested mapping fits a source kind that declares nothing under its word

## Context and Problem Statement

[0096](0096-a-mapping-synthesises-its-subject-slot.md) moved the subject slot
off the source kind and onto the mapping, and the catalog door was not moved
with it: `fitProblems` (`internal/catalog/suggested.go`) still opened with "the
source kind declares no property `person`, the subject reference this mapping
fills". So every shipped provider — which now deliberately declares no slot at
all — made every suggested mapping BLOCKED, the doors dropped each one from the
batch, and three `internal/catalog` tests failed at the tip of the branch
(T-032 in geoah/mneme-v6). A reader installing GitHub and importing `people`
got the kinds and no projection, with a problem naming a declaration nobody is
supposed to write.

## Considered Options

- Delete the subject check from the door and let the loader refuse whatever
  does not fit
- Re-read "fits" against 0096: an undeclared slot fits, a declaration of the
  source's own is the collision
- Hold the door to the loader's full rule by resolving the mapping through a
  candidate registry before the apply

## Decision Outcome

Chosen: the second. The door's check is a NECESSARY condition, not admission —
its whole job is to keep a mapping the reader never wrote from refusing the
whole import — so it has to mirror the loader's refusals and nothing more.
Post-0096 those are:

1. **An undeclared slot fits.** The mapping synthesises the reference when it
   installs, which is what lets a provider ship mirrors without knowing the
   word its consumer will use. This is now the ordinary case, and the shipped
   `github`, `google` and `linear` closures are all of it.
2. **A declaration of the source's OWN under that word blocks** (0096's rule
   3): the mapping would otherwise take a declared property over silently. The
   problem names the declaration and the two ways out — upgrade the provider
   past it, or map under another name.
3. **A declared `subject: true` reference fits** (0096's rule 4): the
   declaration stands and the mapping stamps the pin onto it. Only the shapes
   the loader refuses ON TOP of that adoption are reported — a pin at a kind
   that is not the one this mapping fills, a repeated or keyed slot, a
   cascading one.

The pin is compared against the spellings the door may land the target under,
because a mapping's `to` is the SHIPPED sample kind for a verbatim install and
the REHOMED one for an import, and a pre-0096 provider's pin names whichever of
the two its author had.

The third option was rejected as the same work admission already does, one
transaction later and with no repository to do it against; the first because
the door would then let a blocked mapping into the batch and cost the reader
the import, which is the failure this check exists to prevent.

### Consequences

- Good, because a fresh repository that installs a provider and imports a
  sample gets the projection, which is what both 0048 and 0096 promise it.
- Good, because "blocked by an older provider" now means something a reader can
  act on: a word the provider spends on its own property, a pin it set itself,
  or a property too old to carry what the mapping reads.
- Bad, because the door and the loader state the same three rules in two
  places, in two vocabularies (a parsed `Property` there, a definition map
  here), and they can drift. The door is deliberately the looser of the two —
  the failure it must never have is blocking a mapping that would admit.
- Bad, because a repository whose provider predates 0096 and pins the slot at
  the shipped sample kind reads as READY (the read view accepts both
  spellings) and is dropped by the import door, which commits to the rehomed
  one. The states disagree until the provider is upgraded.

### Confirmation

`internal/catalog`'s `TestASuggestedMappingWaitsThenIsReadyThenLands`,
`TestImportingTasksWithLinearInstalledLandsTheIssueMapping` and
`TestTheVerbatimInstallReportsTheShippedSpelling` hold the fit against the
shipped closures — they are the three that failed under the old rule.
`TestASuggestedMappingIsBlockedByAnOlderProvider` is re-read against this
record: its three cases are the collision, the pin, and a mirror too old to
carry a mapped property, and each still lets the import through without the
mapping. `TestASuggestedMappingFitsASourceThatStillDeclaresTheSlot` is 0096's
rule 4 at the door.

## More Information

Amends [0049](0049-the-owner-of-a-mappings-target-declares-it.md) and
[0096](0096-a-mapping-synthesises-its-subject-slot.md) where the catalog is
concerned: 0096 supersedes item 3 of 0049 for the loader, and this record is
that same supersession carried into the door that publishes a sample's
suggested mappings. Nothing about who declares a mapping, the one-per-(source
kind, subject property) key, or the uninstall refusal changes.

Reopen if the door ever needs the loader's answer exactly: that is a candidate
registry resolved per read, and it is a different cost model from the one this
check was written for.
