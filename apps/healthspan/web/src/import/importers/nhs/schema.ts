// What an nhsctl run holds, as Effect Schemas: the run's manifest, the
// envelope each recorded response is written in, and the bodies of the NHS
// App's answers the importer reads (apps/healthspan/docs/importers/
// 05-nhs.md, What it reads). Only the fields read are declared; a field the
// importer reads that is not what the app sends fails the read, naming the
// file and the field.

import { Schema } from "effect"

const Text = Schema.optional(Schema.NullOr(Schema.String))
const Count = Schema.optional(Schema.NullOr(Schema.Number))
const Flag = Schema.optional(Schema.NullOr(Schema.Boolean))
/** A code id the GP system sends as a number (a `bigint` past what a float
 * holds exactly), or now and then as text. */
const CodeId = Schema.optional(
  Schema.NullOr(Schema.Union([Schema.Number, Schema.BigInt, Schema.String]))
)
const Texts = Schema.optional(
  Schema.NullOr(Schema.Array(Schema.NullOr(Schema.String)))
)
const listOf = <S extends Schema.Top>(item: S) =>
  Schema.optional(Schema.NullOr(Schema.Array(item)))

// ---- the run ----

/** One recorded response, as the manifest lists it. */
export const ResponseEntry = Schema.Struct({
  file: Schema.String,
  bodyFile: Text,
  url: Schema.String,
  status: Schema.Number,
  contentType: Text,
})
export type ResponseEntry = typeof ResponseEntry.Type

export const Manifest = Schema.Struct({
  tool: Text,
  startedAt: Schema.String,
  session: Schema.optional(
    Schema.NullOr(
      Schema.Struct({ name: Text, nhsNumber: Text, gpOdsCode: Text })
    )
  ),
  responses: Schema.Array(ResponseEntry),
})
export type Manifest = typeof Manifest.Type

/** The file one response is written in: the entry, and its JSON body. */
export const Envelope = Schema.Struct({
  body: Schema.optional(Schema.Unknown),
})

// ---- the GP record (`/v1/patient/my-record`) ----

const DateValue = Schema.Struct({ value: Text, datePart: Text })
export type DateValue = typeof DateValue.Type

const TestValue = Schema.Struct({
  term: Schema.String,
  supplierTypeCode: Text,
  displayValue: Text,
  numericValue: Count,
  numericOperator: Text,
  units: Text,
  range: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        minimum: Schema.optional(
          Schema.NullOr(Schema.Union([Schema.String, Schema.Number]))
        ),
        maximum: Schema.optional(
          Schema.NullOr(Schema.Union([Schema.String, Schema.Number]))
        ),
      })
    )
  ),
  associatedTexts: listOf(
    Schema.NullOr(Schema.Union([Schema.String, Schema.Struct({ text: Text })]))
  ),
})
export type TestValue = typeof TestValue.Type

/** One test of the record's test results: a report's worth of values. */
export const TestResult = Schema.Struct({
  id: Schema.String,
  term: Text,
  date: Schema.Struct({ value: Schema.String, datePart: Text }),
  testResultValues: listOf(TestValue),
})
export type TestResult = typeof TestResult.Type

const TestSection = Schema.Struct({
  hasAllHistoricResults: Flag,
  data: listOf(TestResult),
})

const Observation = Schema.Struct({
  term: Text,
  codeId: CodeId,
  eventGuid: Text,
  associatedTexts: Texts,
})

const Consultation = Schema.Struct({
  effectiveDate: DateValue,
  doneBy: Text,
  consultantLocation: Text,
  location: Text,
  consultationHeaders: listOf(
    Schema.Struct({
      header: Text,
      observationsWithTerm: listOf(Observation),
      associatedTexts: Texts,
    })
  ),
})
export type Consultation = typeof Consultation.Type

const Problem = Schema.Struct({
  effectiveDate: DateValue,
  lineItems: listOf(
    Schema.Struct({
      text: Schema.String,
      lineItems: listOf(
        Schema.NullOr(
          Schema.Union([Schema.String, Schema.Struct({ text: Text })])
        )
      ),
    })
  ),
})
export type Problem = typeof Problem.Type

const Allergy = Schema.Struct({
  date: DateValue,
  name: Schema.String,
  reaction: Text,
  drug: Text,
})
export type Allergy = typeof Allergy.Type

const Immunisation = Schema.Struct({
  effectiveDate: DateValue,
  term: Schema.String,
  nextDate: Schema.optional(Schema.NullOr(Schema.Struct({ value: Text }))),
  associatedText: Texts,
})
export type Immunisation = typeof Immunisation.Type

const Medicine = Schema.Struct({
  date: Text,
  lineItems: listOf(Schema.Struct({ text: Schema.String })),
})
export type Medicine = typeof Medicine.Type

const RecordDocument = Schema.Struct({
  effectiveDate: DateValue,
  documentIdentifier: Schema.String,
  term: Text,
  codeId: CodeId,
  comments: Text,
  extension: Text,
})
export type RecordDocument = typeof RecordDocument.Type

const section = <S extends Schema.Top>(item: S) =>
  Schema.optional(Schema.NullOr(Schema.Struct({ data: listOf(item) })))

export const MyRecord = Schema.Struct({
  response: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        testResults: Schema.optional(Schema.NullOr(TestSection)),
        historicTestResults: Schema.optional(Schema.NullOr(TestSection)),
        consultations: section(Consultation),
        problems: section(Problem),
        allergies: section(Allergy),
        immunisations: section(Immunisation),
        medications: Schema.optional(
          Schema.NullOr(
            Schema.Struct({
              data: Schema.optional(
                Schema.NullOr(
                  Schema.Struct({
                    currentRepeatMedications: listOf(Medicine),
                    discontinuedRepeatMedications: listOf(Medicine),
                    acuteMedications: listOf(Medicine),
                  })
                )
              ),
            })
          )
        ),
        documents: section(RecordDocument),
      })
    )
  ),
})
export type MyRecord = typeof MyRecord.Type

// ---- the app's other answers ----

/** `/v1/api/organisation/<ODS code>`: the GP practice. */
export const Organisations = Schema.Array(
  Schema.Struct({
    id: Schema.String,
    name: Schema.String,
    address: Schema.optional(
      Schema.NullOr(
        Schema.Struct({
          address1: Text,
          address2: Text,
          address3: Text,
          city: Text,
          postcode: Text,
        })
      )
    ),
  })
)

/** `/v1/patient/nominated-pharmacy`. */
export const NominatedPharmacy = Schema.Struct({
  pharmacyDetails: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        odsCode: Text,
        pharmacyName: Text,
        addressLine1: Text,
        addressLine2: Text,
        addressLine3: Text,
        city: Text,
        postcode: Text,
        telephoneNumber: Text,
        url: Text,
      })
    )
  ),
})

const EpsItem = Schema.Struct({
  name: Text,
  quantity: Text,
  status: Text,
  statusDate: Text,
})

const Prescription = Schema.Struct({
  barcodeID: Schema.String,
  date: Text,
  pharmacyODSCode: Text,
  pharmacyName: Text,
  pharmacyAddressText: Text,
  pharmacyPostCode: Text,
  pharmacyTelephoneNumber: Text,
  pharmacyUrl: Text,
  prescriberName: Text,
  prescribingOrganizationOdsCode: Text,
  medications: listOf(EpsItem),
})
export type Prescription = typeof Prescription.Type

/** `/api/v1/prescriptions`: the electronic prescriptions. */
export const Prescriptions = Schema.Struct({
  prescriptions: listOf(Prescription),
  pastPrescriptions: listOf(Prescription),
})

const Wait = Schema.Struct({
  odsCode: Text,
  provider: Text,
  specialty: Text,
  referredDate: Text,
  plannedWaitTime: Count,
  eightiethPercentileWaitTime: Count,
  estimatedTreatmentDate: Text,
  censusDate: Text,
})
export type Wait = typeof Wait.Type

/** `/v1/SecondaryCare/summary/`: the hospital waiting lists. */
export const SecondaryCare = Schema.Struct({ waitTimes: listOf(Wait) })

/** `/v1/patient/demographics`. */
export const Demographics = Schema.Struct({
  response: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        patientName: Text,
        nhsNumber: Text,
        dateOfBirth: Text,
        sex: Text,
      })
    )
  ),
})

/** `/v1/patient/pds-record`: the home address. */
export const PdsRecord = Schema.Struct({
  currentHomeAddress: Schema.optional(
    Schema.NullOr(Schema.Struct({ lines: Texts, postalCode: Text }))
  ),
})

const Course = Schema.Struct({
  id: Schema.String,
  name: Schema.String,
  lastIssueDate: Text,
  nextIssueDate: Text,
})
export type Course = typeof Course.Type

/** `/v1/patient/courses`: the repeat courses. */
export const Courses = Schema.Struct({ courses: listOf(Course) })

const Appointment = Schema.Struct({
  id: Schema.String,
  type: Text,
  clinicians: Texts,
  sessionName: Text,
  location: Text,
  telephoneNumber: Text,
  startTime: Text,
  endTime: Text,
})
export type Appointment = typeof Appointment.Type

/** `/v1/patient/appointments`. */
export const Appointments = Schema.Struct({
  pastAppointments: listOf(Appointment),
  upcomingAppointments: listOf(Appointment),
})

const Message = Schema.Struct({
  id: Schema.String,
  sender: Text,
  body: Text,
  sentTime: Text,
  version: Count,
})
export type Message = typeof Message.Type

/** `/v2/api/users/me/messages`: one page of the inbox. */
export const MessagePage = Schema.Struct({ messages: listOf(Message) })
