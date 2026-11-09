// What mychartctl writes for one MyChart portal, one Schema per file read:
// only the fields the importer reads, each optional and nullable where
// MyChart leaves it out or empties it, so a field that is there but not
// what MyChart promises (a number where text belongs) fails the read and
// names the file and the field (apps/healthspan/docs/importers/
// 06-mychart.md, What it reads).

import { Schema } from "effect"

const opt = <S extends Schema.Constraint>(s: S) =>
  Schema.optional(Schema.NullOr(s))

const Text = opt(Schema.String)
const Flag = opt(Schema.Boolean)
const Count = opt(Schema.Number)
const Name = opt(Schema.Struct({ name: Text }))

/** `manifest.json`: the portal, and the runs that wrote the folder. */
export const Manifest = Schema.Struct({
  tool: Schema.String,
  provider: Text,
  baseUrl: Schema.String,
  items: opt(Schema.Record(Schema.String, Schema.Unknown)),
  runs: Schema.Array(
    Schema.Struct({ startedAt: Schema.String, finishedAt: Text })
  ),
})
export type Manifest = typeof Manifest.Type

const ReferenceRange = Schema.Struct({
  displayLow: Text,
  displayHigh: Text,
  formattedReferenceRange: Text,
})

const Component = Schema.Struct({
  componentInfo: opt(
    Schema.Struct({
      name: Text,
      commonName: Text,
      units: Text,
      componentID: Text,
    })
  ),
  componentResultInfo: opt(
    Schema.Struct({
      value: Text,
      referenceRange: opt(ReferenceRange),
      abnormalFlagCategoryValue: Text,
    })
  ),
  componentComments: opt(Schema.Struct({ hasContent: Flag })),
})
export type Component = typeof Component.Type

const Result = Schema.Struct({
  key: Schema.String,
  name: Text,
  orderMetadata: Schema.Struct({
    resultType: Text,
    prioritizedInstantISO: Text,
    specimensDisplay: Text,
    resultTimestampDisplay: Text,
    resultingLab: Name,
    resultStatus: Text,
    readingProviderName: Text,
  }),
  resultComponents: opt(Schema.Array(Component)),
  reportDetails: opt(
    Schema.Struct({
      reportVars: opt(Schema.Struct({ ordDat: Text })),
    })
  ),
  scans: opt(Schema.Array(Schema.Struct({ dcsId: Text, fileExtension: Text }))),
})
export type Result = typeof Result.Type

/** `items/test-results/<id>.json`: one order and its results. */
export const TestResult = Schema.Struct({
  orderName: Text,
  results: opt(Schema.Array(Result)),
})
export type TestResult = typeof TestResult.Type

/** `items/test-result-reports/<result key>.json`, and
 * `items/clinical-notes/<id>.json`: a report's HTML. */
export const Report = Schema.Struct({ reportContent: Text })
export type Report = typeof Report.Type

/** `items/visits/<csn>.json`: one appointment, past or upcoming. */
export const Visit = Schema.Struct({
  Csn: Text,
  Dat: Text,
  Instant: Text,
  VisitTypeName: Text,
  IsPastVisit: Flag,
  IsCanceled: Flag,
  IsNoShow: Flag,
  EncounterIsEDVisit: Flag,
  EncounterType: Count,
  HasDuration: Flag,
  DurationInMinutes: Count,
  ChiefComplaint: Text,
  PrimaryDepartment: opt(
    Schema.Struct({
      Name: Text,
      Specialty: opt(Schema.Struct({ Title: Text })),
      Address: opt(Schema.Array(Schema.NullOr(Schema.String))),
      PhoneNumber: Text,
    })
  ),
  Providers: opt(Schema.Array(Schema.Struct({ Name: Text, IsPerson: Flag }))),
  Organization: opt(
    Schema.Struct({
      OrganizationIdentifier: Text,
      OrganizationName: Text,
      DiscreteAddress: opt(
        Schema.Struct({
          StreetAddress: opt(Schema.Array(Schema.NullOr(Schema.String))),
          City: Text,
          Zip: Text,
          Country: Text,
        })
      ),
    })
  ),
})
export type Visit = typeof Visit.Type

const VisitDocument = Schema.Struct({ dcsID: Text, reportName: Text })
export type VisitDocument = typeof VisitDocument.Type

/** `items/visit-details/<csn>.json`: a past visit's documents. */
export const VisitDetails = Schema.Struct({
  avsInfo: opt(
    Schema.Struct({
      primaryAvs: opt(VisitDocument),
      additionalDocuments: opt(Schema.Array(VisitDocument)),
    })
  ),
})
export type VisitDetails = typeof VisitDetails.Type

/** `items/visit-notes/<csn>.json`: the notes shared for a visit. */
export const VisitNotes = Schema.Struct({
  noteList: opt(
    Schema.Array(
      Schema.Struct({
        hnoID: Text,
        displayName: Text,
        iso: Text,
        provider: Name,
        isAddendum: Flag,
      })
    )
  ),
})
export type VisitNotes = typeof VisitNotes.Type

/** `lists/letters.json`: the letters, with their dates, authors and
 * visits. */
export const Letters = Schema.Struct({
  letters: opt(
    Schema.Array(
      Schema.Struct({
        hnoId: Schema.String,
        csn: Text,
        dateISO: Text,
        empId: Text,
        reason: Text,
      })
    )
  ),
  users: opt(Schema.Record(Schema.String, Schema.NullOr(Name))),
})
export type Letters = typeof Letters.Type

/** `items/letters/<id>.json`: a letter's HTML. */
export const Letter = Schema.Struct({ bodyHTML: Text })

const Pharmacy = Schema.Struct({
  id: Text,
  name: Text,
  formattedAddress: opt(Schema.Array(Schema.NullOr(Schema.String))),
})

/** `items/medications/<id>.json`: a prescription, or a medicine the list
 * names as prescribed elsewhere. */
export const Medication = Schema.Struct({
  id: Text,
  name: Text,
  sig: Text,
  prescriptionNumber: Text,
  startDate: Text,
  authorizingProvider: Name,
  refillDetails: opt(
    Schema.Struct({
      writtenDispenseQuantity: Text,
      refillExpirationDate: Text,
      refillsRemainingKey: Text,
      hasRefillsRemaining: Flag,
      isRefillable: Flag,
      lastDispense: opt(
        Schema.Struct({ dispenseDate: Text, dispenseQuantity: Text })
      ),
      owningPharmacy: opt(Pharmacy),
    })
  ),
})
export type Medication = typeof Medication.Type

const ClinicalItem = Schema.Struct({
  name: Text,
  reactionList: opt(Schema.Array(Schema.Struct({ title: Text }))),
})

/** `items/allergies/<id>.json`. */
export const Allergy = Schema.Struct({
  allergyItem: opt(ClinicalItem),
  localItem: opt(ClinicalItem),
})

/** `items/health-issues/<id>.json`. */
export const HealthIssue = Schema.Struct({
  healthIssueItem: opt(ClinicalItem),
  localItem: opt(ClinicalItem),
})

/** `items/messages/<conversation id>.json`: one conversation. */
export const Conversation = Schema.Struct({
  subject: Text,
  userOverrideNames: opt(
    Schema.Record(Schema.String, Schema.NullOr(Schema.String))
  ),
  messages: opt(
    Schema.Array(
      Schema.Struct({
        wmgId: Schema.String,
        author: opt(
          Schema.Struct({ empKey: Text, wprKey: Text, displayName: Text })
        ),
        body: Text,
        deliveryInstantISO: Text,
      })
    )
  ),
})
export type Conversation = typeof Conversation.Type

/** `lists/personal-details.json`. */
export const PersonalDetails = Schema.Struct({
  legalSex: opt(Schema.Struct({ labelText: Text })),
})

/** `lists/personal-contact.json`. */
export const PersonalContact = Schema.Struct({
  currentValues: opt(
    Schema.Struct({
      address: opt(Schema.Struct({ street: Text, city: Text, zip: Text })),
      phoneNumbers: opt(Schema.Array(Schema.Struct({ phoneNumber: Text }))),
      emailAddress: Text,
    })
  ),
})

/** `lists/health-upkeep.json`. */
export const HealthUpkeep = Schema.Struct({
  pageInfo: opt(Schema.Struct({ dateOfBirthISO: Text })),
})

/** `lists/personal-relationships.json`. */
export const Relationships = Schema.Struct({
  contacts: opt(Schema.Array(Schema.Unknown)),
})

/** `lists/health-summary.json`. */
export const HealthSummary = Schema.Struct({
  header: opt(Schema.Struct({ height: opt(Schema.Unknown) })),
})
