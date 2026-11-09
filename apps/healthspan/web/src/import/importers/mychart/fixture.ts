// An invented mychartctl export for the importer's tests, in MyChart's
// exact shape (field names and nesting as mychartctl writes them), every
// value made up. Only tests import it.

import { pdfFixture } from "../testing"

export const HOST = "mychart.example-hospital.nhs.uk"

/** The run the plan's revision is the start of: 2026-03-10T09:00:00Z. */
export const REVISION = 1773133200

const json = (v: unknown) => JSON.stringify(v, null, 2)

/** A PNG's first bytes: its signature and its header chunk, which is all
 * the importer reads of one; `fill` makes two images of one shape differ. */
export function png(width: number, height: number, fill = 0): Uint8Array {
  const out = new Uint8Array(40)
  out.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  out.set([0, 0, 0, 13, 0x49, 0x48, 0x44, 0x52], 8)
  const view = new DataView(out.buffer)
  view.setUint32(16, width)
  view.setUint32(20, height)
  out[39] = fill
  return out
}

const result = (r: {
  key: string
  name: string
  type?: string
  at?: string
  issued?: string
  ordDat?: string
  lab?: string | null
  specimen?: string
  status?: string
  reader?: string
  components?: unknown[]
  scans?: { dcsId: string; fileExtension: string }[]
}) => ({
  key: r.key,
  name: r.name,
  showName: true,
  orderMetadata: {
    orderProviderName: "Dr Ada Example",
    resultTimestampDisplay: r.issued ?? "02 Mar 2026 14:05",
    prioritizedInstantISO: r.at ?? "2026-03-02T09:30:00+00:00",
    specimensDisplay: r.specimen ?? "Blood (Venous)",
    resultStatus: r.status ?? "Final",
    resultType: r.type ?? "LAB",
    readingProviderName: r.reader ?? "",
    ...(r.lab === null
      ? {}
      : { resultingLab: { name: r.lab ?? "Example Pathology" } }),
  },
  resultComponents: r.components ?? [],
  reportDetails: { reportVars: { ordDat: r.ordDat ?? "" } },
  scans: r.scans ?? [],
})

const component = (c: {
  id?: string
  name: string
  common?: string
  units?: string
  value: string
  low?: string
  high?: string
  formatted?: string
  flag?: string
  comment?: boolean
}) => ({
  componentInfo: {
    componentID: c.id ?? "",
    name: c.name,
    commonName: c.common ?? "",
    units: c.units ?? "",
  },
  componentResultInfo: {
    value: c.value,
    isValueRtf: false,
    referenceRange: {
      displayLow: c.low ?? "",
      displayHigh: c.high ?? "",
      formattedReferenceRange: c.formatted ?? "",
      lowerBoundExclusive: false,
      upperBoundExclusive: false,
    },
    abnormalFlagCategoryValue: c.flag ?? "Unknown",
  },
  componentComments: {
    hasContent: !!c.comment,
    contentAsString: c.comment ? "A comment" : "",
  },
})

const visit = (v: {
  csn: string
  dat: string
  ms: number
  type: string
  past: boolean
  department?: string
  specialty?: string
  providers?: { Name: string; IsPerson: boolean }[]
  ed?: boolean
  cancelled?: boolean
  duration?: number
}) => ({
  Csn: v.csn,
  Dat: v.dat,
  Instant: `/Date(${v.ms})/`,
  VisitTypeName: v.type,
  IsPastVisit: v.past,
  IsCanceled: !!v.cancelled,
  IsNoShow: false,
  EncounterIsEDVisit: !!v.ed,
  EncounterType: v.ed ? 5 : 1,
  HasDuration: v.duration !== undefined,
  DurationInMinutes: v.duration ?? 0,
  ChiefComplaint: "",
  PrimaryDepartment: {
    Name: v.department ?? "Example Clinic",
    Address: ["‪1 Example Road‬", "London", "AB1 2CD"],
    PhoneNumber: "‪020 0000 0000‬",
    Specialty: { Title: v.specialty ?? "Medicine - Example" },
  },
  Providers: v.providers ?? [],
  Organization: {
    OrganizationIdentifier: "EXAMPLE-ORG-1",
    OrganizationName: "Example MyChart",
    DiscreteAddress: {
      StreetAddress: ["2 Example Street"],
      City: "London",
      Zip: "AB1 3CD",
      Country: "GB",
    },
  },
})

const LETTER = `<html><head><title>Letter</title><style>p{}</style></head><body>
<p><b>Example Hospital</b></p>
<table><tr><td>Hospital No: 1234567</td><td>NHS No: 999 999 9999</td></tr>
<tr><td>Patient Name: Ada Example</td></tr>
<tr><th>GP practice identifier</th><td>A12345</td></tr></table>
<h1>Diagnosis</h1><ul><li>First&nbsp;item</li><li><p>Nested words</p></li></ul>
<p>Seen by patient Ada Example on 1/3/2026</p>
<p>Yours &amp; sincerely,<br/>Dr Chris Example</p>
</body></html>`

/** The files of an invented export, by path. */
export function exportFiles(): Record<string, string | Uint8Array> {
  const files: Record<string, string | Uint8Array> = {
    "manifest.json": json({
      tool: "mychartctl dev",
      provider: "example",
      baseUrl: `https://${HOST}/MyChart`,
      items: {},
      files: {},
      lists: {},
      runs: [
        { startedAt: "2026-03-01T10:00:00.123456789Z", finishedAt: "x" },
        { startedAt: "2026-03-10T09:00:00.9Z", finishedAt: "x" },
      ],
    }),
    // a lab order: two values, a value in words, an unmapped component
    "items/test-results/ORDER-A.json": json({
      orderName: "FULL BLOOD COUNT",
      key: "ORDER-A",
      results: [
        result({
          key: "RES-A",
          name: "FULL BLOOD COUNT",
          ordDat: "DAT-1",
          components: [
            component({
              id: "C-HB",
              name: "Haemoglobin (g/L)",
              common: "HB",
              units: "g/L",
              value: "175",
              low: "130",
              high: "170",
              flag: "High",
            }),
            component({
              name: "C-reactive protein",
              common: "CRP",
              units: "mg/L",
              value: "<5",
              formatted: "<5",
              comment: true,
            }),
            component({
              id: "C-AKI",
              name: "Acute Kidney Injury Warning",
              value: "No AKI alert",
            }),
            component({
              id: "C-MYST",
              name: "Mystery marker",
              units: "U/L",
              value: "3",
            }),
            component({
              id: "C-RATIO",
              name: "Cholesterol:HDL ratio",
              common: "CHOL/HDL",
              units: "",
              value: "3.2",
            }),
          ],
        }),
      ],
    }),
    // an order filed later the same morning: its TSH repeats the one of
    // ORDER-C, read earlier, so it holds no value of its own
    "items/test-results/ORDER-B.json": json({
      orderName: "THYROID STIMULATING IMMUNOGLOBULIN",
      results: [
        result({
          key: "RES-B",
          name: "TSI",
          at: "2026-03-02T09:30:00+00:00",
          ordDat: "DAT-1",
          components: [
            component({
              id: "C-TSH",
              name: "Thyroid stimulating hormone",
              common: "TSH",
              units: "mIU/L",
              value: "2.10",
              low: "0.27",
              high: "4.20",
              flag: "Normal",
            }),
          ],
        }),
      ],
    }),
    "items/test-results/ORDER-C.json": json({
      orderName: "THYROID PROFILE",
      results: [
        result({
          key: "RES-0",
          name: "THYROID PROFILE",
          at: "2026-03-02T09:30:00+00:00",
          issued: "29 Mar 2026 01:30",
          specimen: "",
          lab: null,
          components: [
            component({
              id: "C-TSH",
              name: "Thyroid stimulating hormone",
              common: "TSH",
              units: "mIU/L",
              value: "2.10",
              low: "0.27",
              high: "4.20",
              flag: "Normal",
            }),
          ],
        }),
      ],
    }),
    // an imaging result with two scans, filed at no visit: it belongs to
    // the outpatient attendance an hour before it
    "items/test-results/ORDER-D.json": json({
      orderName: "XR CHEST",
      results: [
        result({
          key: "RES-IMG",
          name: "XR CHEST",
          type: "IMAGING",
          at: "2026-03-05T11:00:00+00:00",
          reader: "Dr Robin Example",
          specimen: "",
          scans: [
            { dcsId: "SCAN-1", fileExtension: "PDF" },
            { dcsId: "SCAN-2", fileExtension: "JPG" },
            { dcsId: "SCAN-3", fileExtension: "TIF" },
          ],
        }),
      ],
    }),
    "items/test-result-reports/RES-IMG.json": json({
      reportContent:
        "<div><h4>Report</h4><p>No acute&#160;change.</p><p>Seen by patient Ada Example on 6/3/2026 10:00</p></div>",
    }),
    "files/test-results/SCAN-1.pdf": pdfFixture([
      "BT /F1 12 Tf 72 700 Td (Scan one) Tj ET",
    ]),
    "files/test-results/SCAN-2.jpg": Uint8Array.of(
      0xff,
      0xd8,
      0xff,
      0xe0,
      1,
      2
    ),
    // visits: an attended clinic with its summaries, the outpatient
    // attendance of the scan, an emergency attendance, an upcoming
    // telephone appointment, a cancelled one
    "items/visits/CSN-1.json": json(
      visit({
        csn: "CSN-1",
        dat: "DAT-1",
        ms: Date.parse("2026-03-02T09:00:00Z"),
        type: "Clinic/Practice Visit",
        past: true,
        duration: 20,
        providers: [
          { Name: "Dr Ada Example", IsPerson: true },
          { Name: "Clinic Room 1", IsPerson: false },
        ],
      })
    ),
    "items/visits/CSN-2.json": json(
      visit({
        csn: "CSN-2",
        dat: "DAT-2",
        ms: Date.parse("2026-03-05T10:00:00Z"),
        type: "Hospital Outpatient Visit",
        past: true,
        department: "Example Imaging",
        specialty: "",
      })
    ),
    "items/visits/CSN-3.json": json(
      visit({
        csn: "CSN-3",
        dat: "DAT-3",
        ms: Date.parse("2026-02-01T23:30:00Z"),
        type: "Emergency",
        past: true,
        ed: true,
        department: "Emergency Department",
      })
    ),
    "items/visits/CSN-4.json": json(
      visit({
        csn: "CSN-4",
        dat: "DAT-4",
        ms: Date.parse("2026-07-01T08:30:00Z"),
        type: "Telephone Follow Up Visit",
        past: false,
        duration: 15,
        providers: [{ Name: "Dr Chris Example", IsPerson: true }],
      })
    ),
    "items/visits/CSN-5.json": json(
      visit({
        csn: "CSN-5",
        dat: "DAT-5",
        ms: Date.parse("2026-01-10T10:00:00Z"),
        type: "Video Visit",
        past: false,
        cancelled: true,
      })
    ),
    "items/visit-details/CSN-1.json": json({
      csn: "CSN-1",
      avsInfo: {
        primaryAvs: { dcsID: "AVS-1", reportName: "After Visit Summary" },
        additionalDocuments: [
          { dcsID: "AVS-MISSING", reportName: "Patient Instructions" },
        ],
      },
    }),
    "items/visit-details/CSN-2.json": json({
      csn: "CSN-2",
      avsInfo: { additionalDocuments: [] },
    }),
    "items/visit-details/CSN-3.json": json({
      csn: "CSN-3",
      avsInfo: {
        primaryAvs: { dcsID: "AVS-3", reportName: "ED Discharge Summary" },
      },
    }),
    "files/visit-documents/AVS-1.pdf": pdfFixture([
      "BT /F1 12 Tf 72 700 Td (After Visit Summary) Tj ET\nBT /F1 10 Tf 72 680 Td (You spoke with us on 2 March 2026.) Tj ET",
    ]),
    "files/visit-documents/AVS-3.pdf": pdfFixture([
      "BT /F1 12 Tf 72 700 Td (You saw Dr Robin Example.) Tj ET",
    ]),
    "items/visit-notes/CSN-1.json": json({
      noteList: [
        {
          hnoID: "NOTE-1",
          displayName: "Progress Note",
          iso: "2026-03-02T10:15:00+00:00",
          provider: { name: "Dr Ada Example" },
          isAddendum: true,
        },
      ],
    }),
    "items/clinical-notes/NOTE-1.json": json({
      reportContent: "<p>Doing well.</p><p></p><p>Review in a year.</p>",
    }),
    "lists/letters.json": json({
      letters: [
        {
          hnoId: "LET-1",
          csn: "CSN-1",
          dateISO: "2026-03-03",
          empId: "EMP-1",
          reason: "",
        },
        { hnoId: "LET-GONE", csn: "", dateISO: "2026-03-04", empId: "" },
      ],
      users: { "EMP-1": { empId: "EMP-1", name: "Dr Ada Example" } },
    }),
    "items/letters/LET-1.json": json({ bodyHTML: LETTER }),
    "files/letters/LET-1-1.png": png(600, 100),
    // medicines: a prescription dispensed, one not yet, one prescribed
    // elsewhere (no id)
    "items/medications/MED-1.json": json({
      id: "MED-1",
      name: "exampleol 10mg tablets",
      sig: "Take ONE tablet daily",
      prescriptionNumber: "RX-1",
      startDate: "2 March 2026",
      authorizingProvider: { name: "Dr Ada Example" },
      refillDetails: {
        writtenDispenseQuantity: "28 tablet",
        refillExpirationDate: "2 September 2026",
        refillsRemainingKey: "numrefills_expired",
        hasRefillsRemaining: false,
        isRefillable: false,
        lastDispense: {
          dispenseDate: "3 March 2026",
          dispenseQuantity: "28 tablet",
        },
        owningPharmacy: {
          id: "PH-1",
          name: "Example Pharmacy",
          formattedAddress: ["Unit 1", "3 Example Lane", "London", "AB1 4CD"],
        },
      },
    }),
    "items/medications/MED-2.json": json({
      id: "MED-2",
      name: "examplamide cream",
      sig: "",
      prescriptionNumber: "RX-2",
      startDate: "9 March 2026",
      authorizingProvider: { name: "" },
      refillDetails: {
        writtenDispenseQuantity: "",
        refillExpirationDate: "9 April 2026",
        refillsRemainingKey: "",
        hasRefillsRemaining: false,
        isRefillable: false,
        lastDispense: { dispenseDate: "", dispenseQuantity: "" },
        owningPharmacy: { id: "", name: "" },
      },
    }),
    "items/medications/name-otherol-5mg.json": json({
      id: "",
      name: "otherol 5mg",
      sig: "One at night",
      prescriptionNumber: "",
      startDate: "",
      refillDetails: null,
    }),
    "items/allergies/ALLERGY-1.json": json({
      allergyItem: {
        id: "",
        name: "Examplecillin",
        reactionList: [{ title: "Rash" }, { title: "" }],
      },
    }),
    "items/health-issues/ISSUE-1.json": json({
      healthIssueItem: {},
      localItem: { id: "", name: "Example condition" },
    }),
    // conversations: a booking message with a banner, the owner's reply
    // with an image, a provider's message with a map
    "items/messages/CONV-1.json": json({
      subject: "Your appointment on 2/3/2026",
      userOverrideNames: { "EMP-1": "Dr Ada Example" },
      messages: [
        {
          wmgId: "MSG-1",
          author: { empKey: "EMP-1", displayName: "" },
          body: '<div><img src="Image/Load?fileName=a"><p>See you&nbsp;soon.</p><img src="Image/Load?fileName=b"></div>',
          deliveryInstantISO: "2026-02-20T09:00:00Z",
        },
        {
          wmgId: "MSG-OWN",
          author: { wprKey: "WPR-1", displayName: "" },
          body: '<p>Thanks <img src="Image/Load?fileName=c"></p>',
          deliveryInstantISO: "2026-02-20T10:00:00Z",
        },
        {
          wmgId: "MSG-2",
          author: { empKey: "EMP-9", displayName: "Example Team" },
          body: '<p>The map:</p><img src="Image/Load?fileName=d">',
          deliveryInstantISO: "2026-02-21T09:00:00Z",
        },
      ],
    }),
    "files/messages/CONV-1-1.png": png(800, 200),
    "files/messages/CONV-1-2.png": png(1200, 900, 1),
    "files/messages/CONV-1-3.png": png(1000, 1000, 2),
    "files/messages/CONV-1-4.png": png(1000, 800, 3),
    // three more booking messages: one large square banner in each, which
    // repeats in more than half of the messages carrying images
    "items/messages/CONV-2.json": json({
      subject: "Your appointment on 5/3/2026",
      messages: [
        {
          wmgId: "MSG-3",
          author: { empKey: "", displayName: "" },
          body: '<p>Booked.</p><img src="Image/Load?fileName=e">',
          deliveryInstantISO: "2026-02-22T09:00:00Z",
        },
      ],
    }),
    "files/messages/CONV-2-1.png": png(1000, 1000, 9),
    "items/messages/CONV-3.json": json({
      subject: "Your appointment on 1/7/2026",
      messages: [
        {
          wmgId: "MSG-4",
          author: { empKey: "", displayName: "" },
          body: '<p>Booked.</p><img src="Image/Load?fileName=f">',
          deliveryInstantISO: "2026-06-01T09:00:00+01:00",
        },
      ],
    }),
    "files/messages/CONV-3-1.png": png(1000, 1000, 9),
    "items/messages/CONV-4.json": json({
      subject: "Your appointment on 10/1/2026",
      messages: [
        {
          wmgId: "MSG-5",
          author: { empKey: "", displayName: "" },
          body: '<p>Booked.</p><img src="Image/Load?fileName=g">',
          deliveryInstantISO: "2026-01-02T09:00:00Z",
        },
      ],
    }),
    "files/messages/CONV-4-1.png": png(1000, 1000, 9),
    "lists/personal-details.json": json({ legalSex: { labelText: "Female" } }),
    "lists/personal-contact.json": json({
      currentValues: {
        address: { street: "4 Example Way", city: "London", zip: "AB1 5CD" },
        phoneNumbers: [{ phoneNumber: "" }, { phoneNumber: "‪07000 000000‬" }],
        emailAddress: "ada@example.com",
      },
    }),
    "lists/health-upkeep.json": json({
      pageInfo: { dateOfBirthISO: "1980-01-01" },
    }),
    "lists/personal-relationships.json": json({
      contacts: [{ name: "Chris Example" }],
    }),
    "lists/histories.json": json({}),
    "lists/health-summary.json": json({
      header: { height: { value: "1.70 m" } },
    }),
    "lists/immunizations.json": json({ organizationImmunizationList: [] }),
  }
  return files
}
