// The NHS App's clinical record as `clinical/*` records: the care providers
// it names, the patient registration, consultations, problems, allergies,
// vaccinations, medicines and their dispensing, appointments, waiting-list
// referrals, documents and the inbox (apps/healthspan/docs/importers/
// 05-nhs.md, The clinical record). Every builder writes every property it
// can, null where the record gives nothing, so a later export clears what
// an earlier one set.

import { KIND } from "../../../kinds"
import {
  address,
  atDate,
  emisCoding,
  firstLine,
  keyHash,
  KINDS,
  nhsNumberIdentifier,
  numberedKeys,
  organizationKey,
  organizationRecord,
  parseClock,
  phone,
  absoluteUrl,
  precision,
  recordKey,
  ref,
  strip,
  tidyName,
  zoned,
  dateText,
  docRef,
  type OrganizationInput,
} from "../shared"
import type { PlannedDocument, PlannedRecord } from "../types"
import {
  acuteStatus,
  dayNumber,
  dispenseStatus,
  epsMatches,
  first25,
  sameMedicine,
  vaccineGiven,
  type Given,
} from "./medicines"
import type {
  Appointment,
  Consultation,
  Course,
  Immunisation,
  Medicine,
  Message,
  MyRecord,
  Prescription,
  Problem,
  RecordDocument,
  Wait,
} from "./schema"

/** The zone the NHS App writes a prescription item's status date in. */
export const NHS_ZONE = "Europe/London"

/** The health service a registration is with. */
export const SERVICE = "NHS England"

const SEX: Readonly<Record<string, string>> = {
  male: "male",
  female: "female",
  other: "other",
  unknown: "unknown",
  "not known": "unknown",
  "not specified": "unknown",
}

const SIGNIFICANCE: Readonly<Record<string, string>> = {
  significant: "major",
  major: "major",
  minor: "minor",
}

type Record_ = NonNullable<MyRecord["response"]>

/** The care providers the export names, by ODS code, in the order they
 * are first met: the GP practice, the nominated pharmacy, each pharmacy an
 * electronic prescription went to, each hospital on a waiting list. */
export function careProviders(input: {
  organisations?: readonly {
    id: string
    name: string
    address?: {
      address1?: string | null
      address2?: string | null
      address3?: string | null
      city?: string | null
      postcode?: string | null
    } | null
  }[]
  pharmacy?: {
    odsCode?: string | null
    pharmacyName?: string | null
    addressLine1?: string | null
    addressLine2?: string | null
    addressLine3?: string | null
    city?: string | null
    postcode?: string | null
    telephoneNumber?: string | null
    url?: string | null
  } | null
  prescriptions: readonly Prescription[]
  waits: readonly Wait[]
}): Map<string, OrganizationInput> {
  const orgs = new Map<string, OrganizationInput>()
  for (const o of input.organisations ?? []) {
    const a = o.address
    orgs.set(o.id, {
      ods: o.id,
      name: tidyName(o.name) || o.id,
      type: "gppractice",
      address: address(
        [a?.address1, a?.address2, a?.address3],
        a?.city,
        a?.postcode
      ),
    })
  }
  const p = input.pharmacy
  if (p?.odsCode)
    orgs.set(p.odsCode, {
      ods: p.odsCode,
      name: tidyName(p.pharmacyName ?? "") || p.odsCode,
      type: "pharmacy",
      address: address(
        [p.addressLine1, p.addressLine2, p.addressLine3],
        p.city,
        p.postcode
      ),
      phone: phone(p.telephoneNumber),
      url: absoluteUrl(p.url),
    })
  for (const x of input.prescriptions) {
    const code = x.pharmacyODSCode
    if (!code || orgs.has(code)) continue
    orgs.set(code, {
      ods: code,
      name: tidyName(x.pharmacyName ?? "") || code,
      type: "pharmacy",
      address: address([x.pharmacyAddressText], null, x.pharmacyPostCode),
      phone: phone(x.pharmacyTelephoneNumber),
      url: absoluteUrl(x.pharmacyUrl),
    })
  }
  for (const w of input.waits) {
    const code = w.odsCode
    if (!code || orgs.has(code)) continue
    orgs.set(code, {
      ods: code,
      name: tidyName(w.provider ?? "") || code,
      type: "hospital",
    })
  }
  return orgs
}

/** Planned records by kind and key: a second record under a key replaces
 * the first and is written where the second came, as the later of two
 * rows the record files under one key wins. */
class Records {
  private byKey = new Map<string, PlannedRecord>()

  add(r: PlannedRecord): void {
    const id = `${r.kind}\u0000${r.namespace}\u0000${r.key}`
    this.byKey.delete(id)
    this.byKey.set(id, r)
  }

  list(): PlannedRecord[] {
    return [...this.byKey.values()]
  }
}

/** Everything the clinical record is built from. */
export interface ClinicalInput {
  namespace: string
  /** The run's start, every record's revision but a message's. */
  revision: number
  /** The run's start as a UTC date, `YYYY-MM-DD`: when an issue's
   * validity is judged. */
  now: string
  session: {
    name?: string | null
    nhsNumber?: string | null
    gpOdsCode?: string | null
  }
  orgs: ReadonlyMap<string, OrganizationInput>
  /** The nominated pharmacy's ODS code. */
  pharmacy?: string | null
  record: Record_
  demographics?: {
    patientName?: string | null
    nhsNumber?: string | null
    dateOfBirth?: string | null
    sex?: string | null
  } | null
  home?: {
    lines?: readonly (string | null)[] | null
    postalCode?: string | null
  } | null
  courses: readonly Course[]
  prescriptions: readonly Prescription[]
  appointments: {
    past: readonly Appointment[]
    upcoming: readonly Appointment[]
  }
  waits: readonly Wait[]
  /** The inbox, each message once (the last copy, where the first came). */
  messages: readonly Message[]
  /** Each document's file, by the document's id. */
  files: ReadonlyMap<string, { document: PlannedDocument; text: string | null }>
  /** The test results written, by test id, which a consultation names. */
  reports: ReadonlySet<string>
  ownerContact?: string
}

/** The care providers' records. */
export function organizationRecords(
  orgs: ReadonlyMap<string, OrganizationInput>,
  revision: number
): PlannedRecord[] {
  return [...orgs.values()].map((o) => organizationRecord(o, revision))
}

/** Every clinical record but the care providers, in write order. */
export function clinicalRecords(c: ClinicalInput): PlannedRecord[] {
  const out = new Records()
  const ns = c.namespace
  const orgRef = (code: string | null | undefined) =>
    code && c.orgs.has(code) ? ref(organizationKey(code)) : null
  const practiceCode = c.session.gpOdsCode
  const practice = orgRef(practiceCode)
  const write = (
    kind: string,
    key: string,
    properties: Record<string, unknown>,
    revision = c.revision
  ) => {
    const r: PlannedRecord = {
      ...recordKey(kind, ns, key),
      mode: "maintain",
      properties,
      revision,
    }
    out.add(r)
    return r
  }

  // ---- the registration ----
  const demo = c.demographics ?? {}
  const number = demo.nhsNumber || c.session.nhsNumber
  const patient: Record<string, unknown> = {
    service: SERVICE,
    name: demo.patientName || c.session.name || null,
    identifiers: number ? [nhsNumberIdentifier(number)] : null,
    birthDate: (demo.dateOfBirth ?? "").slice(0, 10) || null,
    gender: SEX[strip(demo.sex ?? "").toLowerCase()] ?? null,
    address: address(c.home?.lines ?? [], null, c.home?.postalCode),
    practice,
    pharmacy: orgRef(c.pharmacy),
  }
  if (c.ownerContact) patient.about = `${KIND.contact}/${c.ownerContact}`
  write(KINDS.patient, "patient", patient)

  // ---- consultations ----
  const consultations = c.record.consultations?.data ?? []
  const keys = numberedKeys(
    consultations.map((x) =>
      keyHash(x.effectiveDate.value, x.doneBy, x.consultantLocation)
    )
  )
  consultations.forEach((x, i) =>
    write(KINDS.encounter, keys[i], consultation(x, practice, c.reports, ns))
  )

  // ---- the problem list ----
  for (const p of c.record.problems?.data ?? []) {
    const { key, properties } = problem(p, practice)
    write(KINDS.condition, key, properties)
  }

  // ---- allergies and vaccinations ----
  for (const a of c.record.allergies?.data ?? [])
    write(KINDS.allergy, keyHash(a.date.value, a.name), {
      name: a.name,
      at: a.date.value ?? null,
      precision: precision(a.date.datePart),
      organization: practice,
      reactions: a.reaction ? [{ description: a.reaction }] : null,
      substance: a.drug || null,
    })
  const given: Given[] = []
  for (const i of c.record.immunisations?.data ?? []) {
    given.push({
      date: (i.effectiveDate.value ?? "").slice(0, 10),
      term: i.term,
    })
    write(
      KINDS.immunization,
      keyHash(i.effectiveDate.value, i.term),
      immunisation(i, practice)
    )
  }

  // ---- medicines ----
  const requests = new Map<string | null, string>()
  const groups = c.record.medications?.data
  const plan: [readonly Medicine[], "continuous" | "acute", string | null][] = [
    [groups?.currentRepeatMedications ?? [], "continuous", "active"],
    [groups?.discontinuedRepeatMedications ?? [], "continuous", "stopped"],
    [groups?.acuteMedications ?? [], "acute", null],
  ]
  for (const [items, course, status] of plan)
    for (const m of items) {
      const lines = (m.lineItems ?? []).map((l) => l.text)
      const name = lines.length ? lines[0] : null
      const matched =
        course === "continuous" && name
          ? c.courses.find(
              (k) => k.name.startsWith(first25(name)) || name.startsWith(k.name)
            )
          : undefined
      const key = matched ? matched.id : keyHash(m.date, name)
      if (matched) requests.set(matched.name, key)
      if (!requests.has(name)) requests.set(name, key)
      let state = status
      if (state === null) {
        const day = (m.date ?? "").slice(0, 10)
        const issued = dayNumber(day) === undefined ? null : day
        state = acuteStatus(
          issued,
          c.now,
          epsMatches(name, issued, c.prescriptions),
          vaccineGiven(name, issued, given)
        )
      }
      write(KINDS.medicationrequest, key, {
        medication: name,
        dosage: lines.length > 1 ? lines[1] : null,
        quantity: lines.length > 2 ? lines[2] : null,
        course,
        status: state,
        intent: course === "continuous" ? "plan" : "order",
        at: m.date ?? null,
        lastIssuedAt: atDate(matched?.lastIssueDate),
        nextIssueAt: atDate(matched?.nextIssueDate),
        organization: practice,
      })
    }
  for (const p of c.prescriptions)
    (p.medications ?? []).forEach((item, i) => {
      const word = strip(item.status ?? "")
      const status = dispenseStatus(word)
      const statusAt = statusTime(item.statusDate)
      const name = item.name ?? ""
      let request: string | undefined
      for (const [nm, key] of requests)
        if (nm && name && sameMedicine(nm, name)) {
          request = key
          break
        }
      const issued = atDate(p.date)
      const handed = status === "completed" ? statusAt : null
      write(KINDS.medicationdispense, `${p.barcodeID}/${i + 1}`, {
        medication: item.name ?? null,
        quantity: item.quantity ?? null,
        status,
        sourceStatus: word || null,
        statusAt,
        prescription: p.barcodeID,
        at: handed ?? issued,
        issuedAt: issued,
        request: request
          ? ref(recordKey(KINDS.medicationrequest, ns, request))
          : null,
        pharmacy: orgRef(p.pharmacyODSCode),
        prescriber: p.prescriberName ?? null,
        organization: orgRef(p.prescribingOrganizationOdsCode) ?? practice,
      })
    })

  // ---- appointments and referrals ----
  for (const [items, past] of [
    [c.appointments.past, true],
    [c.appointments.upcoming, false],
  ] as const)
    for (const a of items)
      write(KINDS.appointment, a.id, appointment(a, past, practice))
  for (const w of c.waits) {
    const provider = tidyName(w.provider ?? "") || w.odsCode || null
    write(KINDS.referral, keyHash(w.referredDate, w.odsCode, w.specialty), {
      name: w.specialty ? `${w.specialty} at ${provider}` : provider,
      specialty: w.specialty ?? null,
      provider: orgRef(w.odsCode),
      status: "active",
      at: w.referredDate ?? null,
      plannedWait: weeks(w.plannedWaitTime),
      typicalWait: weeks(w.eightiethPercentileWaitTime),
      estimatedTreatmentAt:
        (w.estimatedTreatmentDate ?? "").slice(0, 10) || null,
      waitsAsOf: (w.censusDate ?? "").slice(0, 10) || null,
      organization: practice,
    })
  }

  // ---- documents, with their files ----
  for (const d of c.record.documents?.data ?? [])
    write(
      KINDS.document,
      d.documentIdentifier,
      document(d, practice, c.files.get(d.documentIdentifier))
    )

  // ---- the inbox ----
  const practiceName = (
    (practiceCode && c.orgs.get(practiceCode)?.name) ||
    ""
  ).toLowerCase()
  for (const m of c.messages)
    write(
      KINDS.communication,
      m.id,
      {
        sender: m.sender ? tidyName(m.sender) : (m.sender ?? null),
        topic: firstLine(m.body),
        body: m.body ?? null,
        at: m.sentTime ?? null,
        organization:
          (m.sender ?? "").toLowerCase() === practiceName ? practice : null,
      },
      m.version ?? c.revision
    )

  return out.list()
}

/** A wait in weeks as a duration in days, `P14D`. */
function weeks(n: number | null | undefined): string | null {
  return n === null || n === undefined ? null : `P${Math.trunc(7 * n)}D`
}

/** A prescription item's status date (`02-Nov-2026 14:05:00`, London) with
 * its offset, or null. */
export function statusTime(text: string | null | undefined): string | null {
  if (!text) return null
  const w = parseClock(text, "%d-%b-%Y %H:%M:%S")
  return (w && zoned(w, NHS_ZONE)) ?? null
}

/** The characters Python's `str.splitlines` ends a line at, besides
 * `\r\n` read as one. */
const LINE_ENDS = new Set(
  [0x0a, 0x0b, 0x0c, 0x0d, 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029].map((c) =>
    String.fromCharCode(c)
  )
)

/** A text's first line as Python's `str.splitlines()[0]` gives it. */
export function firstOfLines(text: string): string {
  for (let i = 0; i < text.length; i++)
    if (LINE_ENDS.has(text[i])) return text.slice(0, i)
  return text
}

const CLINICIAN = /^(.*?)\s*\(([^)]*)\)\s*$/

/** A clinician as the record writes one, `EXAMPLE, Chris (Dr)`: the name
 * and the role in brackets. */
export function clinician(who: string): { name: string; role: string | null } {
  const m = CLINICIAN.exec(who)
  return m
    ? { name: strip(m[1]), role: m[2] }
    : { name: strip(who), role: null }
}

/** A code id as its digits: a `bigint` read losslessly as text. */
function codeText(
  id: number | bigint | string | null | undefined
): number | string | null | undefined {
  return typeof id === "bigint" ? id.toString() : id
}

function joined(texts: readonly (string | null)[] | null | undefined): string {
  return (texts ?? []).filter((t): t is string => !!t).join("\n")
}

/** A consultation: its sections' entries in order, the same as one text,
 * and the test results filed at it. */
function consultation(
  x: Consultation,
  practice: unknown,
  tested: ReadonlySet<string>,
  ns: string
): Record<string, unknown> {
  const entries: Record<string, unknown>[] = []
  const lines: string[] = []
  const reports: unknown[] = []
  for (const h of x.consultationHeaders ?? []) {
    const section = strip(h.header ?? "") || null
    lines.push(`**${section ?? "Entry"}**`)
    for (const o of h.observationsWithTerm ?? []) {
      const text = joined(o.associatedTexts) || null
      const entry: Record<string, unknown> = { section }
      entry.term = o.term ?? null
      entry.coding = emisCoding(codeText(o.codeId), o.term)
      entry.text = text
      entry.upstreamId = o.eventGuid ?? null
      entries.push(entry)
      lines.push(`- ${o.term ?? ""}${text ? `: ${text}` : ""}`)
      if (o.eventGuid && tested.has(o.eventGuid))
        reports.push(ref(recordKey(KINDS.labreport, ns, o.eventGuid)))
    }
    const free = joined(h.associatedTexts)
    if (free) {
      entries.push({ section, text: free })
      lines.push(free)
    }
  }
  const who = x.doneBy ?? ""
  const person = who ? clinician(who) : undefined
  const term = entries.find((e) => e.term)?.term as string | undefined
  const withText = entries.find((e) => e.text)?.text as string | undefined
  const summary =
    term ??
    (withText !== undefined
      ? Array.from(firstOfLines(withText)).slice(0, 90).join("")
      : `Consultation${person ? ` with ${person.name}` : ""}`)
  return {
    at: x.effectiveDate.value ?? null,
    precision: precision(x.effectiveDate.datePart),
    summary,
    status: "finished",
    organization: practice,
    location: x.location ?? null,
    participants: person ? [person] : null,
    entries: entries.length
      ? entries.map((e) =>
          Object.fromEntries(Object.entries(e).filter(([, v]) => v !== null))
        )
      : null,
    text: lines.join("\n\n") || null,
    reports: reports.length ? reports : null,
  }
}

/** A problem: its name and the line items that say its significance,
 * status, end and notes. */
function problem(
  p: Problem,
  practice: unknown
): { key: string; properties: Record<string, unknown> } {
  const items = p.lineItems ?? []
  const name = items.length ? items[0].text : null
  const fields = new Map<string, string>()
  const notes: string[] = []
  for (const li of items.slice(1)) {
    const at = li.text.indexOf(":")
    const label = at < 0 ? li.text : li.text.slice(0, at)
    const value = at < 0 ? "" : li.text.slice(at + 1)
    if (label === "Significance" || label === "Status" || label === "Ended")
      fields.set(label, strip(value))
    else if (label === "Notes")
      for (const x of li.lineItems ?? []) {
        const t = typeof x === "string" ? x : x?.text
        if (t) notes.push(t)
      }
  }
  const status = (fields.get("Status") ?? "").toLowerCase()
  let ended: string | null = null
  const end = fields.get("Ended")
  if (end) {
    const w = parseClock(end, "%d %B %Y")
    if (w) ended = `${dateText(w)}T00:00:00Z`
    else notes.push(`Ended: ${end}`)
  }
  return {
    key: keyHash(p.effectiveDate.value, name),
    properties: {
      name,
      at: p.effectiveDate.value ?? null,
      precision: precision(p.effectiveDate.datePart),
      endsAt: ended,
      clinicalStatus:
        status === "active"
          ? "active"
          : status === "past"
            ? ended
              ? "resolved"
              : "inactive"
            : null,
      category: "problemlistitem",
      significance:
        SIGNIFICANCE[(fields.get("Significance") ?? "").toLowerCase()] ?? null,
      sourceStatus: fields.get("Status") ?? null,
      organization: practice,
      notes: notes.join("\n") || null,
    },
  }
}

function immunisation(
  i: Immunisation,
  practice: unknown
): Record<string, unknown> {
  return {
    name: i.term,
    at: i.effectiveDate.value ?? null,
    precision: precision(i.effectiveDate.datePart),
    organization: practice,
    nextDueAt: i.nextDate
      ? (i.nextDate.value ?? "").slice(0, 10) || null
      : null,
    notes: joined(i.associatedText) || null,
  }
}

/** How an appointment happens, from its type's words. */
export function channel(type: string): string | null {
  const low = type.toLowerCase()
  if (low.includes("telephone") || low.includes("phone")) return "telephone"
  if (low.includes("video")) return "video"
  if (low.includes("sms") || low.includes("email") || low.includes("online"))
    return "online"
  if (low.includes("face") || low.includes("f2f")) return "inperson"
  return null
}

function appointment(
  a: Appointment,
  past: boolean,
  practice: unknown
): Record<string, unknown> {
  const type = strip(a.type ?? "")
  const people = (a.clinicians ?? []).filter((x): x is string => !!x)
  return {
    name: type + (people.length ? ` with ${people[0]}` : "") || null,
    type: type || null,
    status: past ? null : "booked",
    channel: channel(type),
    service: a.sessionName ?? null,
    location: a.location ?? null,
    phone: phone(a.telephoneNumber),
    participants: people.length ? people.map((name) => ({ name })) : null,
    at: a.startTime ?? null,
    endsAt: a.endTime ?? null,
    organization: practice,
  }
}

function document(
  d: RecordDocument,
  practice: unknown,
  file: { document: PlannedDocument; text: string | null } | undefined
): Record<string, unknown> {
  const day = (d.effectiveDate.value ?? "").slice(0, 10)
  return {
    name: d.comments || `${d.term || "Document"}, ${day}`,
    type: d.term ?? null,
    coding: emisCoding(codeText(d.codeId), d.term),
    at: d.effectiveDate.value ?? null,
    precision: precision(d.effectiveDate.datePart),
    status: "current",
    notes: d.comments ?? null,
    organization: practice,
    text: file?.text ?? null,
    file: file ? docRef(file.document.digest) : null,
  }
}
