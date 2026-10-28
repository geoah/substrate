---
status: proposed
date: 2026-10-28
decision-makers: George Antoniadis
---

# 0193. The clinical record is a first-party catalogue shaped on FHIR

## Context and Problem Statement

A person's medical record reaches a repository from more than one place: a
GP system through a patient app, a hospital portal, a phone's health
records. Lab results already have one name every source writes
(`substrate.reamde.dev/labs`, [0192](0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)),
but conditions, allergies, vaccinations, medicines, consultations,
appointments, referrals, documents and messages have none. Without one,
each importer declares its own kinds under its own authority, and the same
allergy is a different kind in every repository, as the observation was
before 0192.

## Considered Options

- Each importer keeps its own kinds, in its own package.
- One generic kind holding each FHIR resource as JSON.
- Grow `labs` (or `health`) into the whole record, moving lab reports and
  results to new names.
- A separate first-party catalogue, `clinical`, one kind per FHIR resource,
  requiring `labs` for the test results.

## Decision Outcome

Chosen: a separate `clinical` catalogue under `substrate.reamde.dev`, one
kind per FHIR R4 resource a patient-facing record carries, requiring `labs`,
because FHIR is the shape every clinical export already takes, so a source
maps onto it field by field, and because `labs` keeps the test results it
already holds.

- **Twelve kinds**: `patient`, `organization`, `encounter`, `condition`,
  `allergy`, `immunization`, `medicationrequest`, `medicationdispense`,
  `appointment`, `referral`, `document` and `communication`. Each keeps its
  resource's meaning, property names a FHIR mapper expects (`name` is the
  patient's, `subject` is never a subject line) and value sets: an enum value
  is one lowercase word, so FHIR's code loses its hyphens
  (`entered-in-error` is `enteredinerror`) and keeps a label. An encounter's
  `class`, an appointment's `channel` and an organization's `type` are the
  catalogue's own sets.
- **Test results stay in `labs`.** A lab report and its results are FHIR's
  DiagnosticReport and Observation already, and an `encounter` names the
  reports filed at it. A measurement taken at a consultation is a `health`
  observation, so the catalogue ships no metrics.
- **The subject is `about`**, absent meaning the repository's owner, as on a
  lab report. `patient` holds a health service's identifiers for the person
  and is never the target of the other kinds.
- **Every kind binds `revised`**, and a writer keeps the upstream's id as
  the record id, so a second export rewrites in place.
- **A code is one `coding` object** (system, version, code, display), the
  shape a lab result's analyte already has, not FHIR's list of codings, plus
  `descriptionId` for a SNOMED CT description id, FHIR's `coding-sctdescid`
  extension, because UK GP systems record terms by description id and `code`
  must stay the concept id.
- **Medicines follow GP Connect.** A `medicationrequest` is the
  authorisation (intent `plan`) or an issue of it (intent `order`, `basedOn`
  the plan); a `medicationdispense` is the pharmacy's action, timed when the
  item was handed over.
- **Phone numbers are strings.** The `phone` type is E.164 only, and a
  health service writes a number as its staff typed it, as the people sample
  already found.

Rejected: per-importer kinds, for the reason 0192 gave about observations; a
JSON kind, because nothing in it is a property a filter, a reference or a
title can read; and growing `labs`, because the catalog refuses a kind move
(only the seed's boot upgrade carries one), so renaming the lab kinds a day
after 0192 moved every repository's rows onto them would be a hand migration
in every repository for a name.

### Consequences

- Good, because one allergy is one kind whichever source wrote it, and an
  agent or a console page reads every source's record the same way.
- Good, because an install of `clinical` brings `labs` and `health`, so a
  record and its test results land together.
- Bad, because twelve kinds are twelve declarations to keep upgraded, and a
  resource the record lacks today (procedures, care plans, family history)
  is a later upgrade.
- Bad, because a FHIR value set squeezed into one-word values reads less
  like the standard, and one `coding` drops the second code a resource may
  carry.
- Bad, because a consultation's `entries` are objects, which stay out of
  search, so its writer has to write `text` as well.

### Confirmation

`kinds/observations_test.go` (`TestFirstPartyCataloguesArePublished`,
`TestRevisionKindsBindTheRuleThatHoldsIt`) holds the catalogue's kinds, its
requirement on `labs` and the `revised` binding;
`internal/catalog/firstparty_db_test.go` holds that one install brings
`labs` and `health` and that a record of every kind lands.

## More Information

Builds on [0192](0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md),
[0047](0047-a-kind-lives-in-a-package.md) and
[0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md).
Revisit when a second code system per field is needed, or when a source
writes a resource the twelve do not hold.
