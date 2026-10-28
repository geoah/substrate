# Clinical records

`substrate.reamde.dev/clinical` is the well-known catalogue for the record a
care provider keeps about you: health conditions, allergies, vaccinations,
prescribed and dispensed medicines, consultations, appointments, referrals,
documents and messages. Each kind follows one HL7 FHIR R4 resource, so an
export from a GP system, a hospital portal or a phone's health records maps
onto it field by field
([decision 0193](decisions/0193-the-clinical-record-is-a-first-party-catalogue-shaped-on-fhir.md)).

It is installed like the other catalogues
(`bin/substratectl install substrate.reamde.dev/clinical`, or
`POST /api/v1/catalog/{id}/install`), lands published under
`substrate.reamde.dev`, and requires `labs`, which requires `health`: one
install brings all three ([the well-known catalogues](observations.md#the-well-known-catalogues)).

## The kinds

| Kind | FHIR resource | One record is |
| --- | --- | --- |
| `clinical/patient` | Patient | your registration with one health `service`: your `name` as it holds it, the identifiers it files you under (an NHS number), the GP practice you are registered with and the pharmacy your prescriptions go to |
| `clinical/organization` | Organization | one care provider, such as a GP practice, a hospital, a pharmacy or a lab, with the identifiers its health service gives it (an ODS code) |
| `clinical/encounter` | Encounter | one consultation or other contact: when, how (`class`), where, who took part, the coded `entries` filed under their sections, the same consultation as one markdown `text`, the `appointment` it took place in, and the lab `reports` filed at it |
| `clinical/condition` | Condition | one health condition: onset at `at`, resolution at `endsAt`, `clinicalStatus`, `verificationStatus`, `category` (problem list or consultation diagnosis) and `significance` |
| `clinical/allergy` | AllergyIntolerance | one allergy or intolerance: the substance, `type`, `category`, `criticality`, `verificationStatus` and the `reactions` it caused |
| `clinical/immunization` | Immunization | one vaccination: the vaccine, `status`, the dose of the course and when the next is due |
| `clinical/medicationrequest` | MedicationRequest | one prescribed medicine: the authorisation (`intent` plan, a repeat for its whole course) or one issue of it (`intent` order, `basedOn` the plan), with the product, `dosage`, `quantity`, `course` and `status` |
| `clinical/medicationdispense` | MedicationDispense | one item of an issued prescription as the pharmacy handles it: `status`, the pharmacy, the prescription it belongs to, and `at` when it was handed over |
| `clinical/appointment` | Appointment | one appointment, past or upcoming: its slot, `status`, `channel` and who it is with |
| `clinical/referral` | ServiceRequest | one referral: the specialty and provider, `status`, `priority` and the published waits |
| `clinical/document` | DocumentReference | one document, such as a letter or a scanned form: the `file`, and its words in `text` |
| `clinical/communication` | Communication | one message a care provider sent you: its `topic`, `body` and attachments |

Test results are not clinical kinds. A blood test is a `labs/labreport`
with its `labs/labresult` values, FHIR's DiagnosticReport and Observation
([the well-known catalogues](observations.md#the-well-known-catalogues)), and a
consultation names the reports filed at it in `reports`. A measurement taken
at a consultation, such as a weight or a blood pressure, is an observation of
a `health` metric. The catalogue ships no metrics of its own.

## Writing a record

A provider or an importer writes these kinds the way the health adapters
write theirs:

- **Ids are the upstream's.** Compose them as an observed row's are
  ([0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)):
  `<provider>/<account>/<upstream id>`, such as
  `nhs/owner/consultation/<event id>`, so a second export rewrites each row
  in place and a second account, such as a linked profile, never collides
  with the first. A care provider is keyed by the identifier its health
  service gives it (`ods/<ODS code>`), shared by every account that names
  it. Every kind binds core's `revised`: write `revision`, and a rewrite
  with a lower one is refused.
- **`about` names the subject.** Absent, it means the repository's owner, as
  on a lab report; the other clinical kinds never point at the `patient`
  record.
- **A code is one `coding`.** `system`, `version`, `code` and `display`, the
  shape a lab result's analyte has. For SNOMED CT (`http://snomed.info/sct`)
  `code` is the concept id; a source that records the term's description id
  instead, as UK GP systems do, writes it in `descriptionId`, FHIR's
  `coding-sctdescid` extension. dm+d is `https://dmd.nhs.uk`. An identifier
  carries the system its service publishes:
  `https://fhir.nhs.uk/Id/nhs-number` for an NHS number,
  `https://fhir.nhs.uk/Id/ods-organization-code` for an ODS code.
- **An enum value is FHIR's code without its hyphens.** `entered-in-error` is
  `enteredinerror`, `problem-list-item` is `problemlistitem`,
  `unable-to-assess` is `unabletoassess`, `continuous-repeating-dispensing`
  is `continuousrepeatingdispensing`, and each carries a label. A status the
  source words in a way no value fits is kept in `sourceStatus`. Three value
  sets are the catalogue's own, because FHIR's are codings a person does not
  read: an encounter's `class`, an appointment's `channel` and an
  organization's `type`.
- **Medicines follow GP Connect.** The authorisation is a
  `medicationrequest` of intent `plan`; an issue of it, where the source
  records issues, is intent `order` with `basedOn` naming the plan; what the
  pharmacy did with an issue is a `medicationdispense`, timed when it was
  handed over, or at `issuedAt` until then.
- **A partial date says so.** A record known only to the year, the month or
  the day sets `precision` (and `endPrecision` for its end), so a reader
  shows `2017` rather than `1 January 2017`.
- **Phone numbers are strings, as written.** The `phone` type takes E.164
  only, and a health service writes `(020) 7946 0958`.
- **Searchable words go in the text properties.** An object, such as a
  consultation's `entries`, stays out of search and embeddings, so a writer
  of a consultation writes `text` too, and a document's words, where a reader
  can extract them, go in its `text`. `notes`, `text` and a message's `body`
  are embedded.

## Not here yet

The catalogue ships no record views and no functions: a consultation's
measurements become `health` observations only where the writer adds them.
Procedures, care plans and family history have no kinds yet; each is an
additive upgrade.
