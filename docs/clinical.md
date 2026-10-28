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
| `clinical/patient` | Patient | your registration with one health service: the identifiers it files you under (an NHS number), the GP practice you are registered with and the pharmacy your prescriptions go to |
| `clinical/organization` | Organization | one care provider, such as a GP practice, a hospital, a pharmacy or a lab, with the identifiers its health service gives it (an ODS code) |
| `clinical/encounter` | Encounter | one consultation or other contact: when, how (`class`), where, who took part, the coded `entries` filed under their sections, the same consultation as one markdown `text`, and the lab `reports` filed at it |
| `clinical/condition` | Condition | one health condition: onset at `at`, resolution at `endsAt`, `clinicalStatus`, `category` (problem list or consultation diagnosis) and `significance` |
| `clinical/allergy` | AllergyIntolerance | one allergy or intolerance: the substance, `type`, `category`, `criticality` and the `reactions` it caused |
| `clinical/immunization` | Immunization | one vaccination: the vaccine, `status`, the dose of the course and when the next is due |
| `clinical/medicationrequest` | MedicationRequest | one prescribed medicine, a repeat for its whole course: product, `dosage`, `quantity`, `course` (one-off, repeat, repeat dispensing) and `status` |
| `clinical/medicationdispense` | MedicationDispense | one item of an issued prescription as the pharmacy handles it: `status`, the pharmacy and the prescription it belongs to |
| `clinical/appointment` | Appointment | one appointment, past or upcoming: its slot, `status`, `channel` and who it is with |
| `clinical/referral` | ServiceRequest | one referral: the specialty and provider, `status`, `priority` and the published waits |
| `clinical/document` | DocumentReference | one document, such as a letter or a scanned form: the `file`, and its words in `text` |
| `clinical/communication` | Communication | one message a care provider sent you, with its `body` and attachments |

Test results are not clinical kinds. A blood test is a `labs/labreport`
with its `labs/labresult` values, FHIR's DiagnosticReport and Observation
([the well-known catalogues](observations.md#the-well-known-catalogues)), and a
consultation names the reports filed at it in `reports`. A measurement taken
at a consultation, such as a weight or a blood pressure, is an observation of
a `health` metric. The catalogue ships no metrics of its own.

## Writing a record

A provider or an importer writes these kinds the way the health adapters
write theirs:

- **Ids are the upstream's.** Keep the source system's own id in the record
  id (`gp/<event id>`, `eps/<prescription id>/<item>`), so a second export
  rewrites each row in place instead of adding a copy. Every kind binds core's
  `revised`: write `revision`, and a rewrite with a lower one is refused.
- **`about` names the subject.** Absent, it means the repository's owner, as
  on a lab report; the other clinical kinds never point at the `patient`
  record.
- **Codes are one `coding`.** `system`, `code` and `display`, as a lab
  result's analyte is: SNOMED CT is `http://snomed.info/sct` and dm+d
  `https://dmd.nhs.uk`. An identifier carries the system its service
  publishes: `https://fhir.nhs.uk/Id/nhs-number` for an NHS number,
  `https://fhir.nhs.uk/Id/ods-organization-code` for an ODS code.
- **A partial date says so.** A record known only to the year, the month or
  the day sets `precision`, so a reader shows `2017` rather than
  `1 January 2017`.
- **An enum value is one lowercase word.** FHIR's codes keep their meaning
  and drop their hyphens: `entered-in-error` is `enteredinerror`,
  `in-progress` is `inprogress`, `on-hold` is `onhold`, `not-done` is
  `notdone`, and each carries a label. A status the source words in a way no
  value fits is kept in `sourceStatus`.
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
