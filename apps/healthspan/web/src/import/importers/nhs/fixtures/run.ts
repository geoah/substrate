// An invented nhsctl run, in the exact shape nhsctl writes one: the
// manifest, and one envelope per recorded response (`responses/NNNN-<host>-
// <path>.json`, the body inside), a document's file beside its envelope.
// Every name, number, id and value is made up.

/** One response the invented run recorded. */
export interface Recorded {
  method?: string
  url: string
  status?: number
  contentType?: string
  /** The JSON body, written into the envelope. */
  body?: unknown
  /** JSON text for the body, where it must hold what JavaScript cannot
   * write (an integer past 2^53). */
  bodyText?: string
  /** A body that is not JSON, written beside the envelope. */
  file?: { ext: string; bytes: Uint8Array }
}

const API = "https://api.nhsapp.service.nhs.uk"

/** A run folder's files, by their paths inside it. */
export function runFiles(
  startedAt: string,
  responses: readonly Recorded[],
  session: Record<string, unknown> = SESSION
): Record<string, string | Uint8Array> {
  const files: Record<string, string | Uint8Array> = {}
  const entries = responses.map((r, i) => {
    const seq = i + 1
    const path = r.url.replace(/^https:\/\/([^/]+)/, "$1 ").split("?")[0]
    const slug = path
      .replace(".nhsapp.service.nhs.uk", "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "")
    const name = `${String(seq).padStart(4, "0")}-${slug}`
    const entry: Record<string, unknown> = {
      seq,
      file: `responses/${name}.json`,
      page: "call example",
      method: r.method ?? "GET",
      url: r.url,
      status: r.status ?? 200,
      contentType:
        r.contentType ??
        (r.file
          ? "application/octet-stream"
          : "application/json; charset=utf-8"),
      bytes: 0,
      at: "2026-11-02T08:16:00.000000000Z",
    }
    if (r.file) {
      entry.bodyFile = `responses/${name}.${r.file.ext}`
      entry.bytes = r.file.bytes.length
      files[entry.bodyFile as string] = r.file.bytes
    }
    const head = JSON.stringify(entry, null, 2)
    const body =
      r.bodyText ?? (r.body === undefined ? undefined : JSON.stringify(r.body))
    files[entry.file as string] =
      body === undefined ? head : `${head.slice(0, -2)},\n  "body": ${body}\n}`
    return entry
  })
  files["manifest.json"] = JSON.stringify(
    {
      tool: "nhsctl v0.3.0",
      startedAt,
      finishedAt: startedAt,
      session,
      pages: [],
      calls: [],
      endpoints: {},
      responses: entries,
    },
    null,
    2
  )
  return files
}

export const SESSION = {
  name: "Alex Example",
  nhsNumber: "9990000000",
  gpOdsCode: "A12345",
  proofLevel: "P9",
  appVersion: "example",
}

/** A code id past 2^53, as the GP system sends a SNOMED CT description id:
 * written into the JSON text as digits. */
export const BIG_CODE = "14990931000006116"
const BIG = "__big__"

const date = (value: string, datePart = "YearMonthDayTime") => ({
  value,
  datePart,
})

const value = (
  term: string,
  v: Record<string, unknown> = {}
): Record<string, unknown> => ({
  isHeadlineResult: false,
  description: null,
  term,
  range: { minimum: null, maximum: null, units: null },
  supplierTypeCode: null,
  snomedConceptId: null,
  supportingContent: null,
  displayValue: "",
  numericValue: null,
  numericOperator: null,
  associatedTexts: [],
  units: null,
  ...v,
})

const test = (
  id: string,
  term: string,
  when: ReturnType<typeof date>,
  values: Record<string, unknown>[]
) => ({
  id,
  date: when,
  description: null,
  term,
  supplierTypeCode: "EX1",
  snomedConceptId: null,
  supportingContent: null,
  associatedTexts: [],
  testResultValues: values,
})

/** The record's test results. */
export const TESTS = [
  test("t-fbc", "Full blood count", date("2026-03-30T09:15:00+00:00"), [
    value("Haemoglobin estimation", {
      supplierTypeCode: "1022431000000105",
      displayValue: "152 g/L",
      numericValue: 152.0,
      units: "g/L",
      range: { minimum: "130", maximum: "170", units: null },
    }),
    value("Total white cell count", {
      supplierTypeCode: "1022541000000102",
      displayValue: "6.10 x10^9/L",
      numericValue: 6.1,
      units: "x10^9/L",
      range: { minimum: "4.0", maximum: null, units: null },
    }),
    value("Percentage nucleated RBCs", {
      supplierTypeCode: "1022621000000107",
      displayValue: "0 .",
      numericValue: 0.0,
      units: ".",
    }),
    value("Mystery index", { displayValue: "7", numericValue: 7.0 }),
  ]),
  test("t-kidney", "Kidney function", date("2026-03-30T09:15:00+00:00"), [
    value("eGFRcreat (CKD-EPI)/1.73 m*2", {
      supplierTypeCode: "1011481000000105",
      displayValue: ">90 mL/min",
      numericValue: 90.0,
      numericOperator: ">",
      units: "mL/min",
    }),
    value("Serum creatinine", {
      supplierTypeCode: "1000731000000107",
      displayValue: "",
      numericValue: 72.0,
      units: "umol/L",
      range: { minimum: "59", maximum: "104", units: null },
    }),
  ]),
  test("t-aki", "AKI warning stage", date("2026-03-30T09:15:00+00:00"), [
    value("AKI warning stage", {
      supplierTypeCode: "1032021000000103",
      associatedTexts: [{ text: "No flag", textType: "XX" }],
    }),
  ]),
  test(
    "t-bp",
    "Blood pressure",
    date("2026-02-01T00:00:00+00:00", "YearMonthDay"),
    [
      value("Self reported systolic blood pressure", {
        displayValue: "128 mmHg",
        numericValue: 128.0,
        units: "mmHg",
      }),
      value("Self reported diastolic blood pressure", {
        displayValue: "82 mmHg",
        numericValue: 82.0,
        units: "mmHg",
      }),
    ]
  ),
  test("t-unknown", "Mystery panel", date("2026-01-05T10:00:00+00:00"), [
    value("Mystery index", { displayValue: "3", numericValue: 3.0 }),
  ]),
  {
    ...test("t-ratio", "Lipids", date("2025-11-03T08:30:00+00:00"), [
      value("Serum cholesterol/HDL ratio", {
        supplierTypeCode: "1015681000000109",
        displayValue: "3.2",
        numericValue: 3.2,
      }),
      value("Serum cholesterol/HDL ratio", {
        supplierTypeCode: "1015681000000109",
        displayValue: "3.3",
        numericValue: 3.3,
      }),
    ]),
    snomedConceptId: BIG,
  },
]

/** A test only a year's historic results hold. */
export const HISTORIC_TEST = test(
  "t-2019",
  "Vitamin D",
  date("2019-06-01T10:00:00+00:00"),
  [
    value("Serum total 25-OH vit D level", {
      supplierTypeCode: "1018751000000107",
      displayValue: "60 nmol/L",
      numericValue: 60.0,
      units: "nmol/L",
    }),
  ]
)

const consultation = (
  when: ReturnType<typeof date>,
  doneBy: string,
  headers: unknown[],
  consultantLocation: string | null = doneBy
) => ({
  effectiveDate: when,
  consultantLocation,
  location: "Main surgery",
  doneBy,
  consultationHeaders: headers,
})

const medicine = (when: string, ...lines: string[]) => ({
  date: when,
  lineItems: lines.map((text) => ({ text, lineItems: [] })),
})

/** The GP record, as `/v1/patient/my-record` answers. */
export function myRecord(
  options: { all?: boolean; tests?: unknown[] } = {}
): Record<string, unknown> {
  const section = (data: unknown[], more: Record<string, unknown> = {}) => ({
    hasAccess: true,
    hasErrored: false,
    recordCount: data.length,
    data,
    hasUndeterminedAccess: false,
    ...more,
  })
  return {
    response: {
      hasSummaryRecordAccess: true,
      hasDetailedRecordAccess: true,
      supplier: "EMIS",
      allergies: section([
        {
          name: "Penicillin allergy",
          date: date("2015-01-01T00:00:00+00:00", "Year"),
          drug: "Penicillin",
          reaction: "Rash",
        },
        {
          name: "Penicillin allergy",
          date: date("2015-01-01T00:00:00+00:00", "Year"),
          drug: null,
          reaction: null,
        },
      ]),
      medications: section([]),
      immunisations: section([
        {
          term: "Rabies vaccination",
          associatedText: ["Batch EX123", ""],
          effectiveDate: date("2026-03-30T10:00:00+00:00"),
          status: null,
          nextDate: {
            value: "2029-03-30T00:00:00+00:00",
            datePart: "YearMonthDay",
          },
        },
        {
          term: "Japanese encephalitis vaccination",
          associatedText: [],
          effectiveDate: date("2025-02-10T11:00:00+00:00"),
          status: null,
          nextDate: null,
        },
        {
          term: "Requires a cholera vaccination",
          associatedText: [],
          effectiveDate: date("2026-03-30T10:05:00+00:00"),
          status: null,
          nextDate: null,
        },
      ]),
      testResults: section(options.tests ?? TESTS, {
        hasAllHistoricResults: options.all ?? false,
      }),
      historicTestResults: section([], { hasAllHistoricResults: false }),
      problems: section([
        {
          effectiveDate: date("2020-05-01T00:00:00+00:00", "YearMonth"),
          lineItems: [
            { text: "Asthma", lineItems: [] },
            { text: "Significance: Significant", lineItems: [] },
            { text: "Status: Active", lineItems: [] },
            {
              text: "Notes:",
              lineItems: ["Uses an inhaler", { text: "Reviewed yearly" }],
            },
          ],
        },
        {
          effectiveDate: date("2021-04-12T00:00:00+00:00", "YearMonthDay"),
          lineItems: [
            { text: "Sprained ankle", lineItems: [] },
            { text: "Significance: Minor", lineItems: [] },
            { text: "Status: Past", lineItems: [] },
            { text: "Ended: 3 June 2021", lineItems: [] },
          ],
        },
        {
          effectiveDate: date("2018-01-01T00:00:00+00:00", "Year"),
          lineItems: [
            { text: "Back pain", lineItems: [] },
            { text: "Status: Past", lineItems: [] },
            { text: "Ended: some time in 2019", lineItems: [] },
          ],
        },
      ]),
      tppDcrEvents: {
        hasAccess: true,
        hasErrored: false,
        recordCount: 0,
        errors: null,
        data: [],
      },
      consultations: {
        hasAccess: true,
        hasErrored: false,
        recordCount: 4,
        data: [
          consultation(
            date("2026-03-30T09:20:00.917+00:00"),
            "EXAMPLE, Sam (Dr)",
            [
              {
                header: "History",
                observationsWithTerm: [
                  {
                    term: "Full blood count",
                    associatedTexts: ["Normal", ""],
                    eventGuid: "t-fbc",
                    codeId: BIG,
                    observationType: "Test",
                  },
                  {
                    term: "Mystery panel",
                    associatedTexts: [],
                    eventGuid: "t-unknown",
                    codeId: 1234567,
                    observationType: "Test",
                  },
                ],
                associatedTexts: ["Feeling well"],
              },
              {
                header: " Comment ",
                observationsWithTerm: [
                  {
                    term: "Seen in clinic",
                    associatedTexts: [],
                    eventGuid: "e-2",
                    codeId: 2534664018,
                    observationType: "Observation",
                  },
                ],
                associatedTexts: [],
              },
            ]
          ),
          consultation(
            date("2026-03-30T09:20:00.917+00:00"),
            "EXAMPLE, Sam (Dr)",
            [
              {
                header: "",
                observationsWithTerm: [
                  {
                    term: "Kidney function",
                    associatedTexts: [],
                    eventGuid: "t-kidney",
                    codeId: 1000731000000107,
                    observationType: "Test",
                  },
                ],
                associatedTexts: [],
              },
            ]
          ),
          consultation(
            date("2026-05-05T00:00:00+00:00", "YearMonthDay"),
            "Nurse Example",
            [
              {
                header: "Comment",
                observationsWithTerm: [],
                associatedTexts: ["Line one\nLine two"],
              },
            ],
            null
          ),
          consultation(date("2026-06-01T15:21:00+00:00"), "", []),
        ],
      },
      documents: {
        hasAccess: true,
        hasErrored: false,
        recordCount: 3,
        data: [
          {
            effectiveDate: date("2026-04-02T11:00:00+00:00"),
            documentIdentifier: "doc-1",
            term: "Letter",
            size: 9,
            extension: "PDF",
            isAvailable: true,
            name: "Alex_EXAMPLE_1990-01-02_letter.pdf",
            eventGuid: "ev-doc-1",
            codeId: 123456010,
            type: null,
            isValidFile: true,
            comments: null,
            needMoreInformation: false,
          },
          {
            effectiveDate: date("2026-05-06T12:00:00+00:00"),
            documentIdentifier: "doc-2",
            term: "Discharge summary",
            size: 23,
            extension: "rtf",
            isAvailable: true,
            name: "Alex_EXAMPLE_1990-01-02_summary.rtf",
            eventGuid: "ev-doc-2",
            codeId: BIG,
            type: null,
            isValidFile: true,
            comments: "Summary from the ward",
            needMoreInformation: false,
          },
          {
            effectiveDate: date("2026-07-14T00:00:00+00:00", "YearMonthDay"),
            documentIdentifier: "doc-3",
            term: null,
            size: 0,
            extension: "pdf",
            isAvailable: false,
            name: "Alex_EXAMPLE_1990-01-02_scan.pdf",
            eventGuid: "ev-doc-3",
            codeId: null,
            type: null,
            isValidFile: false,
            comments: null,
            needMoreInformation: false,
          },
        ],
      },
    },
  }
}

/** The medicines the record lists, beside the rest of the record. */
export const MEDICATIONS = {
  hasAccess: true,
  hasErrored: false,
  recordCount: 7,
  errors: null,
  data: {
    acuteMedications: [
      medicine(
        "2026-03-30T00:00:00+00:00",
        "Rabipur vaccine powder and solvent",
        "Use as directed",
        "1 x 0.5ml"
      ),
      medicine("2026-10-27T00:00:00+00:00", "Newmed 2mg tablets", "one daily"),
      medicine("2025-01-15T00:00:00+00:00", "Pastmed 3mg tablets"),
      medicine(
        "2025-02-10T00:00:00+00:00",
        "Ixiaro suspension for injection",
        "Use as directed"
      ),
      medicine(
        "2026-10-19T00:00:00+00:00",
        "Otherine 5mg capsules",
        "two daily",
        "14 capsule"
      ),
    ],
    currentRepeatMedications: [
      medicine(
        "2026-01-10T00:00:00+00:00",
        "Exampleol 10mg tablets",
        "one daily",
        "28 tablet"
      ),
    ],
    discontinuedRepeatMedications: [
      medicine(
        "2025-01-01T00:00:00+00:00",
        "Oldmed 1mg tablets",
        "as directed"
      ),
    ],
  },
  hasUndeterminedAccess: false,
}

/** The GP record's JSON text, with the code ids past 2^53 as digits. */
export function myRecordText(
  options: { all?: boolean; tests?: unknown[] } = {}
): string {
  const r = myRecord(options)
  ;(r.response as Record<string, unknown>).medications = MEDICATIONS
  return JSON.stringify(r).replaceAll(`"${BIG}"`, BIG_CODE)
}

const EPS = {
  totalPrescriptions: 2,
  successfulPrescriptions: 2,
  failedPrescriptions: 0,
  prescriptions: [
    {
      date: "2026-10-20",
      barcodeID: "EX0001-A12345-00001A",
      prescriptionType: "continuous",
      status: "TrackingNotSupported",
      pharmacyODSCode: "FA001",
      pharmacyName: "EXAMPLE CHEMIST",
      pharmacyPostCode: "EX1 2BB",
      pharmacyTelephoneNumber: "01234 567890",
      pharmacyUrl: "www.example-chemist.test",
      pharmacyAddressText: "2 High Street, Exampletown",
      prescriberName: "Dr Sam Example",
      prescribingOrganizationName: "THE EXAMPLE SURGERY",
      prescribingOrganizationOdsCode: "A12345",
      medications: [
        {
          name: "Exampleol 10mg tablets",
          quantity: "28 tablet",
          status: "WithDispenser",
          statusDate: "20-Oct-2026 10:00:00",
          terminalStatus: null,
        },
        {
          name: "Otherine 5mg capsules",
          quantity: "14 capsule",
          status: "NotTracked",
          statusDate: "20-Oct-2026 10:00:00",
          terminalStatus: null,
        },
      ],
      cancelledCount: 0,
      isHistoric: false,
    },
  ],
  pastPrescriptions: [
    {
      date: "2026-03-30",
      barcodeID: "EX0002-A12345-00002B",
      prescriptionType: "acute",
      status: "Collected",
      pharmacyODSCode: "FB002",
      pharmacyName: "SECOND PHARMACY LTD",
      pharmacyPostCode: "OT1 1CC",
      pharmacyTelephoneNumber: "",
      pharmacyUrl: "https://second-pharmacy.test",
      pharmacyAddressText: "3 Market Square, Othertown",
      prescriberName: "Dr Sam Example",
      prescribingOrganizationName: "ELSEWHERE CLINIC",
      prescribingOrganizationOdsCode: "Y99999",
      medications: [
        {
          name: "Rabipur vaccine powder and solvent",
          quantity: "1 x 0.5ml",
          status: "Collected",
          statusDate: "01-Apr-2026 09:30:00",
          terminalStatus: true,
        },
      ],
      cancelledCount: 0,
      isHistoric: true,
    },
  ],
  trackingEnabled: true,
}

/** Every response of the invented run, in the order nhsctl recorded them. */
export function responses(
  options: { all?: boolean; years?: Recorded[]; tests?: unknown[] } = {}
): Recorded[] {
  return [
    {
      url: `${API}/v1/patient/my-record`,
      body: { response: { testResults: { data: [] } } },
    },
    {
      url: `${API}/v1/api/organisation/A12345`,
      body: [
        {
          id: "A12345",
          name: "THE EXAMPLE SURGERY",
          address: {
            address1: "1 EXAMPLE STREET",
            address2: "",
            address3: null,
            city: "EXAMPLETOWN",
            county: "",
            postcode: "EX1 1AA",
          },
        },
      ],
    },
    {
      url: `${API}/v1/patient/nominated-pharmacy`,
      body: {
        pharmacyDetails: {
          odsCode: "FA001",
          pharmacyName: "EXAMPLE CHEMIST",
          pharmacyType: "P1",
          pharmacySubType: "Community",
          addressLine1: "2 High Street",
          addressLine2: " ",
          addressLine3: null,
          city: "Exampletown",
          county: "",
          postcode: "EX1 2BB",
          url: "www.example-chemist.test",
          telephoneNumber: "01234 567890",
          distance: null,
          openingTimes: [],
        },
        nominatedPharmacyEnabled: true,
      },
    },
    {
      url: "https://epsadapterapi.nhsapp.service.nhs.uk/api/v1/prescriptions?fromDate=2000-01-01T00:00:00Z",
      body: EPS,
    },
    {
      url: "https://secondarycareadapterapi.nhsapp.service.nhs.uk/v1/SecondaryCare/summary/",
      body: {
        upcomingAppointments: [],
        historicalAppointments: [],
        referrals: [],
        waitTimes: [
          {
            referredDate: "2026-08-01T00:00:00+00:00",
            plannedWaitTime: 18,
            eightiethPercentileWaitTime: 20,
            provider: "EXAMPLE HOSPITALS NHS TRUST",
            odsCode: "RXX",
            specialty: "Dermatology",
            censusDate: "2026-09-30T00:00:00+00:00",
            estimatedTreatmentDate: "2026-12-05T00:00:00+00:00",
            estimatedTreatmentDisplayDate: "December 2026",
          },
        ],
        attachments: [],
        pifuPlans: [],
        partialErrors: { appointments: false, attachments: false },
      },
    },
    { url: `${API}/v1/patient/secondary-care/summary`, status: 404 },
    {
      url: `${API}/v1/patient/demographics`,
      body: {
        response: {
          patientName: "Alex EXAMPLE",
          dateOfBirth: "1990-01-02T00:00:00",
          sex: "Male",
          address: "10 Example Road, Exampletown, EX1 3DD",
          nhsNumber: "999 000 0000",
        },
      },
    },
    {
      url: `${API}/v1/patient/pds-record`,
      body: {
        currentHomeAddress: {
          lines: ["10 Example Road", " ", "Exampletown"],
          postalCode: "EX1 3DD",
        },
      },
    },
    {
      url: `${API}/v1/patient/courses`,
      body: {
        courses: [
          {
            id: "course-1",
            name: "Exampleol 10mg tablets",
            details: "one daily",
            lastIssueDate: "2026-10-20T00:00:00",
            nextIssueDate: "2026-11-24T00:00:00",
            lastRequested: null,
            requestable: true,
          },
        ],
        specialRequestNecessity: "NotApplicable",
        specialRequestCharacterLimit: 0,
      },
    },
    {
      url: `${API}/v1/patient/appointments`,
      body: {
        pastAppointments: [
          {
            id: "ap-1",
            startTime: "2026-06-01T15:20:00+01:00",
            endTime: "2026-06-01T15:30:00+01:00",
            location: "Main surgery",
            clinicians: ["Dr Sam Example", ""],
            type: " Telephone consultation ",
            sessionName: "Morning clinic",
            channel: 0,
            telephoneNumber: "‪01234 000000‬",
          },
        ],
        upcomingAppointments: [
          {
            id: "ap-2",
            startTime: "2026-11-02T09:00:00+00:00",
            endTime: "2026-11-02T09:10:00+00:00",
            location: "Main surgery",
            clinicians: [],
            type: "Face to face",
            sessionName: "Nurse clinic",
            channel: 0,
            telephoneNumber: null,
          },
        ],
        cancellationReasons: [],
        disableCancellation: false,
        pastAppointmentsEnabled: true,
      },
    },
    { url: `${API}/v1/patient/my-record`, bodyText: myRecordText(options) },
    ...(options.years ?? []),
    {
      method: "POST",
      url: `${API}/v1/documents/doc-1/download`,
      contentType: "application/pdf",
      file: { ext: "pdf", bytes: new TextEncoder().encode("%PDF-1.4 example") },
    },
    {
      method: "POST",
      url: `${API}/v1/documents/doc-2/download`,
      contentType: "text/plain; charset=utf-8",
      file: {
        ext: "txt",
        bytes: new TextEncoder().encode("{\\rtf1 Example summary}"),
      },
    },
    {
      url: `${API}/v2/api/users/me/messages?index=0&count=50&isRemoved=false`,
      body: {
        canLoadMore: false,
        messages: [
          {
            id: "m-1",
            senderId: "s-1",
            sender: "THE EXAMPLE SURGERY",
            version: 3,
            body: "<p>Your results are ready</p>\nPlease sign in to see them.",
            read: true,
            flagged: false,
            removed: false,
            sentTime: "2026-04-01T09:33:35.806Z",
            reply: null,
            hasAttachment: false,
            paperLetterOptedOut: null,
          },
          {
            id: "m-2",
            senderId: "s-2",
            sender: "NHS App",
            version: 1,
            body: "Welcome to the NHS App",
            read: true,
            flagged: false,
            removed: false,
            sentTime: "2026-01-01T12:00:00Z",
            reply: null,
            hasAttachment: false,
            paperLetterOptedOut: null,
          },
        ],
      },
    },
    {
      url: `${API}/v2/api/users/me/messages?index=0&count=50&isRemoved=true`,
      body: {
        canLoadMore: false,
        messages: [
          {
            id: "m-1",
            senderId: "s-1",
            sender: "THE EXAMPLE SURGERY",
            version: 4,
            body: "<p>Your results are ready</p>",
            read: true,
            flagged: false,
            removed: true,
            sentTime: "2026-04-01T09:33:35.806Z",
            reply: null,
            hasAttachment: false,
            paperLetterOptedOut: null,
          },
          {
            id: "m-3",
            senderId: "s-1",
            sender: "THE EXAMPLE SURGERY",
            version: null,
            body: "",
            read: true,
            flagged: false,
            removed: true,
            sentTime: "2026-05-01T08:00:00Z",
            reply: { response: "Thank you", sentTime: "2026-05-01T09:00:00Z" },
            hasAttachment: false,
            paperLetterOptedOut: null,
          },
        ],
      },
    },
  ]
}

/** A year's historic test results, in the section shape the record's own
 * test results take. */
export function year(y: number, tests: unknown[]): Recorded {
  return {
    url: `${API}/v1/patient/historic-test-results/${y}`,
    body: {
      hasAccess: true,
      hasErrored: false,
      recordCount: tests.length,
      data: tests,
      hasUndeterminedAccess: false,
      hasAllHistoricResults: false,
    },
  }
}

/** The run's start: 1793607330 seconds since the epoch (its fraction
 * past six digits dropped, its seconds truncated). */
export const STARTED = "2026-11-02T08:15:30.987654321Z"
export const REVISION = 1793607330

/** The invented run folder's files. */
export function exampleRun(
  options: { all?: boolean; years?: Recorded[]; tests?: unknown[] } = {}
): Record<string, string | Uint8Array> {
  return runFiles(STARTED, responses(options))
}
