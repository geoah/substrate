// A MyChart portal's clinical record: the patient registration, the visits
// as appointments and encounters, the documents filed at them, the health
// issues, allergies and medicines, and the messages a provider sent
// (apps/healthspan/docs/importers/06-mychart.md, The clinical record).

import { KIND } from "../../../kinds"
import {
  address,
  blobDigest,
  cleanText,
  dateIn,
  dateText,
  docRef,
  extensionOf,
  instantIn,
  instantMs,
  isoText,
  KINDS,
  mediaTypeOf,
  NHS_NUMBER_SYSTEM,
  numberedKeys,
  organizationKey,
  parseClock,
  parseIso,
  plannedDocument,
  recordKey,
  ref,
  reportKey,
  strip,
  type Identifier,
} from "../shared"
import type {
  LeftOut,
  PlannedDocRef,
  PlannedDocument,
  PlannedRecord,
  PlannedRef,
} from "../types"
import { safeId } from "./folder"
import { PROVIDER, type ResultOf } from "./labs"
import { markdown } from "./markdown"
import type {
  Conversation,
  Letters,
  Medication,
  Report,
  Visit,
  VisitDetails,
  VisitNotes,
} from "./schemas"

/** One item of a resource as read: mychartctl's file name for it (its
 * file-safe MyChart id) and its decoded JSON. */
export interface Stemmed<A> {
  stem: string
  value: A
}

/** A downloaded file, read: its path in the input, its bytes and, for a
 * visit document, the words pdf.js finds in it. */
export interface LoadedFile {
  path: string
  bytes: Uint8Array
  text?: string | null
}

/** What the clinical record is read from. */
export interface ClinicalExport {
  visits: Stemmed<Visit>[]
  details: Stemmed<VisitDetails>[]
  visitNotes: Stemmed<VisitNotes>[]
  clinicalNotes: Stemmed<Report>[]
  letterList?: Letters
  letters: Stemmed<{ bodyHTML?: string | null }>[]
  results: ResultOf[]
  reports: Stemmed<Report>[]
  medications: Stemmed<Medication>[]
  allergies: Stemmed<ClinicalItemOf<"allergyItem">>[]
  healthIssues: Stemmed<ClinicalItemOf<"healthIssueItem">>[]
  conversations: Stemmed<Conversation>[]
  personalDetails?: { legalSex?: { labelText?: string | null } | null }
  personalContact?: {
    currentValues?: {
      address?: {
        street?: string | null
        city?: string | null
        zip?: string | null
      } | null
      phoneNumbers?: readonly { phoneNumber?: string | null }[] | null
      emailAddress?: string | null
    } | null
  }
  healthUpkeep?: { pageInfo?: { dateOfBirthISO?: string | null } | null }
  /** The file of an upstream id in `files/<resource>/`, read. */
  file(resource: string, id: string): LoadedFile | undefined
}

interface ClinicalItem {
  name?: string | null
  reactionList?: readonly { title?: string | null }[] | null
}
type ClinicalItemOf<K extends string> = {
  [P in K | "localItem"]?: ClinicalItem | null
}

/** What reading the clinical record needs. */
export interface ClinicalContext {
  namespace: string
  zone: string
  revision: number
  /** The day a prescription's validity is judged on, `YYYY-MM-DD`: the
   * newest run's start, in UTC. */
  today: string
  hospital: { name: string; ods: string }
  ownerContact?: string
  /** The keys of the lab reports the plan writes. */
  labReports: ReadonlySet<string>
}

export interface ClinicalRead {
  /** The pharmacies, written with the organizations. */
  organizations: PlannedRecord[]
  /** Everything else, in write order. */
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
}

/** What an after-visit summary says of the contact. */
const SPOKE = /You spoke with us on /
const SAW = /You saw \S/
/** A UK postcode, the last line of an address MyChart formats. */
const POSTCODE = /^[A-Z]{1,2}\d[A-Z\d]? ?\d[A-Z]{2}$/i
/** The visit type of a diagnostic attendance an unfiled imaging result
 * belongs to. */
const OUTPATIENT = "Hospital Outpatient Visit"
const SEX: Readonly<Record<string, string>> = {
  male: "male",
  female: "female",
  unknown: "unknown",
  "not known": "unknown",
}
const IMAGE_REF = /Image\/Load\?fileName=/g
const HOUR = 3_600_000

// ---- times ----

/** A .NET `/Date(<ms>)/` instant as the zone shows it, with its offset. */
export function dotnetInstant(
  text: string | null | undefined,
  zone: string
): string | null {
  const m = /^\/Date\((-?\d+)\)\/$/.exec(text ?? "")
  return m ? (instantIn(Number(m[1]), zone) ?? null) : null
}

/** A stamp `minutes` later, at the stamp's own offset; null where either
 * is missing or zero. */
export function plusMinutes(
  stamp: string | null,
  minutes: number | null | undefined
): string | null {
  if (!stamp || !minutes) return null
  const p = parseIso(stamp)
  if (!p || p.offset === undefined) return null
  const w = p.wall
  const t =
    Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second) +
    Math.trunc(minutes) * 60_000
  const d = new Date(t)
  return isoText(
    {
      year: d.getUTCFullYear(),
      month: d.getUTCMonth() + 1,
      day: d.getUTCDate(),
      hour: d.getUTCHours(),
      minute: d.getUTCMinutes(),
      second: d.getUTCSeconds(),
      microsecond: w.microsecond,
    },
    p.offset
  )
}

/** A displayed date (`22 April 2026`) as that day's start in UTC, the form
 * a datetime holds a date in; null where it is none. */
export function displayedDay(text: string | null | undefined): string | null {
  const s = strip(text ?? "")
  if (!s) return null
  const w = parseClock(s, "%d %B %Y")
  return w ? `${dateText(w)}T00:00:00Z` : null
}

// ---- small readings ----

/** `text` with its first letter upper case and the rest lower. */
function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1).toLowerCase()
}

/** A PNG's width and height, or undefined for anything else. */
export function pngSize(
  bytes: Uint8Array
): { width: number; height: number } | undefined {
  const sig = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]
  if (bytes.length < 24 || sig.some((b, i) => bytes[i] !== b)) return undefined
  if (String.fromCharCode(...bytes.subarray(12, 16)) !== "IHDR")
    return undefined
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  return { width: view.getUint32(16), height: view.getUint32(20) }
}

/** A banner, logo, footer or QR code by its shape: a PNG at least 2.4
 * times as wide as it is tall, or at most 300 pixels on its longer side.
 * Maps, leaflets and forms are larger and squarer. */
export function decorative(bytes: Uint8Array): boolean {
  const size = pngSize(bytes)
  if (!size || !size.height) return false
  return (
    size.width >= 2.4 * size.height || Math.max(size.width, size.height) <= 300
  )
}

/** A prescription's status as of `today` (`YYYY-MM-DD`), from its refill
 * details: `completed` once dispensed with no refill left, `stopped` once
 * expired, `active` while a refill remains or it is still valid or
 * refillable, else `unknown`. */
export function prescriptionStatus(
  refill: NonNullable<Medication["refillDetails"]> | null | undefined,
  today: string
): "completed" | "stopped" | "active" | "unknown" {
  const r = refill ?? {}
  const dispensed = !!strip(r.lastDispense?.dispenseDate ?? "")
  const expiryText = strip(r.refillExpirationDate ?? "")
  const expiryWall = expiryText ? parseClock(expiryText, "%d %B %Y") : undefined
  const expiry = expiryWall && dateText(expiryWall)
  const expired =
    r.refillsRemainingKey === "numrefills_expired" ||
    (expiry !== undefined && expiry < today)
  const remaining = !!r.hasRefillsRemaining
  if (dispensed && !remaining) return "completed"
  if (expired) return "stopped"
  if (remaining || expiry !== undefined || r.isRefillable) return "active"
  return "unknown"
}

/** The hospital number, NHS number, name and GP practice ODS code the
 * letters print, each the value printed most often (the first seen on a
 * tie). */
export function patientNumbers(texts: readonly (string | null)[]): {
  mrn?: string
  nhs?: string
  name?: string
  gp?: string
} {
  const patterns = {
    mrn: /Hospital No[:.]?\s*(\d{6,10})\b/gi,
    nhs: /NHS No[:.]?\s*(\d{3}\s?\d{3}\s?\d{4})\b/gi,
    name: /Patient Name[:.]?\s*([^\n|]+?)\s*(?:\n|\||$)/gi,
    gp: /GP practice identifier\s*\|?\s*([A-Z]\d{5})\b/gi,
  }
  const out: { mrn?: string; nhs?: string; name?: string; gp?: string } = {}
  for (const [key, pattern] of Object.entries(patterns) as [
    keyof typeof patterns,
    RegExp,
  ][]) {
    const counts = new Map<string, number>()
    for (const text of texts)
      for (const m of (text ?? "").matchAll(pattern)) {
        const v = key === "nhs" ? m[1].replace(/ /g, "") : strip(m[1])
        counts.set(v, (counts.get(v) ?? 0) + 1)
      }
    let best: [string, number] | undefined
    for (const e of counts) if (!best || e[1] > best[1]) best = e
    if (best) out[key] = best[0]
  }
  return out
}

/** The clinicians a visit names as people: a clinic, room, scanner or
 * shared login is not a participant. */
function people(v: Visit): { name: string }[] | null {
  const names = (v.Providers ?? [])
    .filter((p) => p.Name && p.IsPerson)
    .map((p) => ({ name: p.Name! }))
  return names.length ? names : null
}

// ---- the record ----

/** The clinical record, in write order. */
export function readClinical(
  ex: ClinicalExport,
  ctx: ClinicalContext
): ClinicalRead {
  const ns = ctx.namespace
  const org = ref(organizationKey(ctx.hospital.ods))
  const left: LeftOut[] = []
  const documents: PlannedDocument[] = []
  const keep = (d: PlannedDocument) => {
    if (!documents.some((x) => x.digest === d.digest)) documents.push(d)
    return docRef(d.digest)
  }
  const record = (
    kind: string,
    key: string,
    properties: Record<string, unknown>
  ): PlannedRecord => ({
    ...recordKey(kind, ns, key),
    mode: "maintain",
    properties,
    revision: ctx.revision,
  })
  const fileDoc = (
    f: LoadedFile,
    kind: string,
    at: string | null,
    mediaType?: string
  ) =>
    keep(
      plannedDocument(f.bytes, {
        provider: PROVIDER,
        kind,
        date: (at && dateIn(at, ctx.zone)) || "",
        mediaType: mediaType ?? mediaTypeOf(f.path),
        ext: extensionOf(f.path) || undefined,
      })
    )

  // ---- the pharmacies that dispensed ----
  const organizations: PlannedRecord[] = []
  const pharmacies = new Map<string, PlannedRecord>()
  for (const { value: m } of ex.medications) {
    const refill = m.refillDetails
    const p = refill?.owningPharmacy
    if (
      !p?.id ||
      !refill?.lastDispense ||
      !Object.keys(refill.lastDispense).length
    )
      continue
    const lines = (p.formattedAddress ?? [])
      .map((l) => strip(l ?? ""))
      .filter(Boolean)
    const postcode =
      lines.length && POSTCODE.test(lines[lines.length - 1])
        ? lines.pop()!
        : null
    const city = lines.length > 1 ? lines.pop()! : null
    const properties: Record<string, unknown> = {
      name: p.name || "Pharmacy",
      type: "pharmacy",
    }
    const where =
      lines.length || postcode ? address(lines, city, postcode) : null
    if (where) properties.address = where
    pharmacies.set(p.id, {
      ...recordKey(KINDS.organization, ns, `pharmacy/${p.id}`),
      mode: "merge",
      properties,
      revision: ctx.revision,
    })
  }
  organizations.push(...pharmacies.values())

  // ---- the registration ----
  const records: PlannedRecord[] = []
  const numbers = patientNumbers(
    ex.letters.map((l) => markdown(l.value.bodyHTML))
  )
  const contact = ex.personalContact?.currentValues ?? {}
  const home = contact.address ?? {}
  const identifiers: Identifier[] = []
  if (numbers.mrn)
    identifiers.push({ value: numbers.mrn, label: "Hospital number" })
  if (numbers.nhs)
    identifiers.push({
      system: NHS_NUMBER_SYSTEM,
      value: numbers.nhs,
      label: "NHS number",
    })
  const phones = (contact.phoneNumbers ?? [])
    .map((p) => p.phoneNumber)
    .filter((p): p is string => !!p)
  const patient: Record<string, unknown> = {
    service: ctx.hospital.name,
    name: numbers.name ?? null,
    identifiers: identifiers.length ? identifiers : null,
    birthDate: ex.healthUpkeep?.pageInfo?.dateOfBirthISO || null,
    gender:
      SEX[strip(ex.personalDetails?.legalSex?.labelText ?? "").toLowerCase()] ??
      null,
    address: home.street ? address([home.street], home.city, home.zip) : null,
    phone: phones.length ? cleanText(phones[0]) : null,
    email: contact.emailAddress || null,
    // the GP practice the letters name, by the ODS code the NHS App's
    // import keys the practice's record on
    practice: numbers.gp
      ? ref(organizationKey(numbers.gp.toUpperCase()))
      : null,
  }
  if (ctx.ownerContact) patient.about = `${KIND.contact}/${ctx.ownerContact}`
  records.push(record(KINDS.patient, "patient", patient))

  // ---- the visits ----
  /** A visit's file name by its file name or its CSN. */
  const visitStem = new Map<string, string>()
  const visitOf = new Map<string, Visit>()
  for (const { stem, value } of ex.visits) {
    visitStem.set(stem, stem)
    visitOf.set(stem, value)
  }
  for (const { stem, value } of ex.visits)
    if (value.Csn && !visitStem.has(value.Csn)) visitStem.set(value.Csn, stem)
  const visit = (id: string | null | undefined) =>
    id ? visitStem.get(id) : undefined
  const details = new Map(ex.details.map((d) => [d.stem, d.value]))
  const datToVisit = new Map<string, string>()
  for (const { stem, value } of ex.visits)
    if (value.Dat) datToVisit.set(value.Dat, stem)

  const labReports = new Map<string, PlannedRef[]>()
  const imagingAt = new Map<string, string>()
  let unwritten = 0
  for (const { result } of ex.results) {
    const stem = datToVisit.get(result.reportDetails?.reportVars?.ordDat ?? "")
    if (!stem) continue
    if (result.orderMetadata.resultType === "LAB") {
      if (!ctx.labReports.has(result.key)) {
        unwritten++
        continue
      }
      const key = reportKey({ namespace: ns, key: result.key })
      labReports.set(stem, [...(labReports.get(stem) ?? []), ref(key)])
    } else imagingAt.set(result.key, stem)
  }
  if (unwritten)
    left.push({
      what: `${unwritten} lab order${unwritten === 1 ? "" : "s"} filed at a visit`,
      why: "no value of the order is written, so neither is its report, and the visit's encounter does not name it",
    })

  const started = (v: Visit) => {
    const at = dotnetInstant(v.Instant, ctx.zone)
    return at ? instantMs(at) : undefined
  }
  // An imaging result whose order MyChart files under no visit belongs to
  // the scan's own attendance: the latest outpatient visit of the same day
  // that began at most two hours before the result.
  for (const { result } of ex.results) {
    const meta = result.orderMetadata
    const iso = meta.prioritizedInstantISO
    if (meta.resultType === "LAB" || imagingAt.has(result.key) || !iso) continue
    const when = instantMs(iso)
    if (when === undefined) continue
    const day = dateIn(iso, ctx.zone)
    let best: { t: number; stem: string } | undefined
    for (const { stem, value: v } of ex.visits) {
      const t = started(v)
      if (
        !details.has(stem) ||
        v.VisitTypeName !== OUTPATIENT ||
        t === undefined
      )
        continue
      const vDay = dateIn(new Date(t).toISOString(), ctx.zone)
      if (
        vDay === day &&
        when - t >= 0 &&
        when - t <= 2 * HOUR &&
        (!best || t > best.t)
      )
        best = { t, stem }
    }
    if (best) imagingAt.set(result.key, best.stem)
  }

  // How a visit happened, as its after-visit summary words it.
  const said = new Map<string, "telephone" | "inperson">()
  for (const { stem, value: d } of ex.details) {
    const avs = d.avsInfo?.primaryAvs
    const f = avs?.dcsID ? ex.file("visit-documents", avs.dcsID) : undefined
    const words = (f?.text ?? "").replace(/\s+/g, " ")
    if (SPOKE.test(words)) said.set(stem, "telephone")
    else if (SAW.test(words)) said.set(stem, "inperson")
  }
  const channelOf = (stem: string, v: Visit) => {
    const s = said.get(stem)
    if (s) return s
    const name = (v.VisitTypeName ?? "").toLowerCase()
    if (name.includes("telephone")) return "telephone"
    if (name.includes("video")) return "video"
    if (name.includes("face to face") || name.includes("outpatient"))
      return "inperson"
    return null
  }

  interface Encounter {
    key: string
    properties: Record<string, unknown>
    lines: string[]
    documents: string[]
  }
  const encounters = new Map<string, Encounter>()
  const appointments: PlannedRecord[] = []
  for (const { stem, value: v } of ex.visits) {
    const key = v.Csn || stem
    const dept = v.PrimaryDepartment ?? {}
    const specialty = dept.Specialty?.Title ?? null
    const at = dotnetInstant(v.Instant, ctx.zone)
    const endsAt = v.HasDuration ? plusMinutes(at, v.DurationInMinutes) : null
    const emergency = !!v.EncounterIsEDVisit || v.EncounterType === 5
    const past = !!v.IsPastVisit
    const where =
      (dept.Address ?? [])
        .map((x) => cleanText(x))
        .filter((x): x is string => !!x)
        .join(", ") || null
    const who = people(v)
    let appointment: PlannedRef | null = null
    if (!emergency) {
      const status = v.IsCanceled
        ? "cancelled"
        : v.IsNoShow
          ? "noshow"
          : past
            ? "fulfilled"
            : "booked"
      const heading = `${v.VisitTypeName || "Appointment"}${who ? ` with ${who[0].name}` : dept.Name ? ` at ${dept.Name}` : ""}`
      const a = record(KINDS.appointment, key, {
        name: heading,
        type: v.VisitTypeName ?? null,
        status,
        channel: channelOf(stem, v),
        service: dept.Name ?? null,
        specialty,
        location: where,
        phone: cleanText(dept.PhoneNumber),
        participants: who,
        reason: v.ChiefComplaint || null,
        at,
        endsAt,
        organization: org,
      })
      appointments.push(a)
      appointment = ref(a)
    }
    if (!past || !details.has(stem)) continue
    encounters.set(stem, {
      key,
      properties: {
        summary: `${v.VisitTypeName || "Visit"}, ${specialty || dept.Name || ctx.hospital.name}`,
        type: v.VisitTypeName ?? null,
        class: emergency ? "emergency" : channelOf(stem, v),
        status: "finished",
        at,
        endsAt,
        organization: org,
        location: dept.Name ?? null,
        participants: who,
        reason: v.ChiefComplaint || null,
        appointment,
        reports: labReports.get(stem) ?? null,
        text: null,
      },
      lines: [
        `**${v.VisitTypeName || "Visit"}**`,
        dept.Name ? `Department: ${dept.Name}` : "",
        specialty ? `Specialty: ${specialty}` : "",
        who ? `Seen by: ${who.map((p) => p.name).join(", ")}` : "",
      ].filter(Boolean),
      documents: [],
    })
  }
  records.push(...appointments)

  // ---- the documents, collected before the encounters are written so
  // each encounter's text can name them ----
  const docs: PlannedRecord[] = []
  const encounterOf = (id: string | undefined) => {
    const stem = visit(id)
    return stem ? encounters.get(stem) : undefined
  }
  const doc = (
    key: string,
    properties: Record<string, unknown>,
    file: PlannedDocRef | null,
    visitId?: string
  ) => {
    const e = encounterOf(visitId)
    if (e) e.documents.push(properties.name as string)
    docs.push(
      record(KINDS.document, key, {
        name: null,
        type: null,
        at: null,
        precision: null,
        author: null,
        notes: null,
        text: null,
        ...properties,
        file,
        encounter: e ? ref(recordKey(KINDS.encounter, ns, e.key)) : null,
        status: "current",
        organization: org,
      })
    )
  }

  // 1. after-visit summaries and discharge documents
  for (const { stem, value: d } of ex.details) {
    const avs = d.avsInfo ?? {}
    const at = (encounters.get(stem)?.properties.at as string | null) ?? null
    for (const a of [
      ...(avs.primaryAvs ? [avs.primaryAvs] : []),
      ...(avs.additionalDocuments ?? []),
    ]) {
      if (!a.dcsID) continue
      const f = ex.file("visit-documents", a.dcsID)
      if (!f) {
        left.push({
          what: `${a.reportName || "A visit document"} of visit ${stem.slice(0, 16)}`,
          why: "its file is not in the export",
        })
        continue
      }
      doc(
        a.dcsID,
        {
          name: a.reportName || "Visit summary",
          type: a.reportName ?? null,
          at,
          text: f.text ?? null,
        },
        fileDoc(
          f,
          "visit document",
          at ?? dotnetInstant(visitOf.get(stem)?.Instant, ctx.zone)
        ),
        stem
      )
    }
  }

  // 2. letters
  const bodies = new Map(ex.letters.map((l) => [l.stem, l.value]))
  const authors = ex.letterList?.users ?? {}
  for (const entry of ex.letterList?.letters ?? []) {
    const body = bodies.get(entry.hnoId) ?? bodies.get(safeId(entry.hnoId))
    if (!body) continue
    const v = visitOf.get(visit(entry.csn) ?? "")
    const specialty = v?.PrimaryDepartment?.Specialty?.Title
    const html = body.bodyHTML ?? ""
    const at = entry.dateISO ? `${entry.dateISO}T00:00:00Z` : null
    doc(
      entry.hnoId,
      {
        name: `Clinic letter${specialty ? `, ${specialty}` : ""}`,
        type: "Letter",
        at,
        precision: "day",
        author: (entry.empId ? authors[entry.empId]?.name : null) ?? null,
        notes: entry.reason || null,
        text: markdown(body.bodyHTML),
      },
      html
        ? keep(
            plannedDocument(new TextEncoder().encode(html), {
              provider: PROVIDER,
              kind: "letter",
              date: entry.dateISO ?? "",
              mediaType: "text/html",
              ext: "html",
            })
          )
        : null,
      entry.csn ?? undefined
    )
  }
  if (ex.letters.length)
    left.push({
      what: `the logos, footers and signatures embedded in ${ex.letters.length} letter${ex.letters.length === 1 ? "" : "s"} (files/letters/)`,
      why: "each letter's HTML is kept as its file, and its words as its text",
    })

  // 3. clinical notes
  const notesOf = new Map<string, { visit: string; note: NoteOf }>()
  for (const { stem, value } of ex.visitNotes)
    for (const note of value.noteList ?? [])
      if (note.hnoID) {
        notesOf.set(note.hnoID, { visit: stem, note })
        notesOf.set(safeId(note.hnoID), { visit: stem, note })
      }
  for (const { stem, value: n } of ex.clinicalNotes) {
    const found = notesOf.get(stem)
    const note = found?.note
    const html = n.reportContent ?? ""
    const at = note?.iso ?? null
    doc(
      stem,
      {
        name: note?.displayName || "Clinical note",
        type: "Clinical note",
        at,
        author: note?.provider?.name ?? null,
        notes: note?.isAddendum ? "An addendum" : null,
        text: markdown(n.reportContent),
      },
      html
        ? keep(
            plannedDocument(new TextEncoder().encode(html), {
              provider: PROVIDER,
              kind: "clinical note",
              date: (at && dateIn(at, ctx.zone)) || "",
              mediaType: "text/html",
              ext: "html",
            })
          )
        : null,
      found?.visit
    )
  }

  // 4. imaging results, their report as the text and each scan a file
  const reports = new Map(ex.reports.map((r) => [r.stem, r.value]))
  for (const { item, result } of ex.results) {
    const meta = result.orderMetadata
    if (meta.resultType === "LAB") continue
    const title = item.orderName || result.name || "Imaging result"
    const scans: { id: string; f: LoadedFile }[] = []
    for (const s of result.scans ?? []) {
      const f = s.dcsId ? ex.file("test-results", s.dcsId) : undefined
      if (s.dcsId && f) scans.push({ id: s.dcsId, f })
    }
    const rep = reports.get(result.key) ?? reports.get(safeId(result.key))
    const at = meta.prioritizedInstantISO ?? null
    const properties = {
      name: title,
      type:
        meta.resultType === "IMAGING"
          ? "Imaging result"
          : capitalize(meta.resultType || "Result"),
      at,
      author: meta.readingProviderName || null,
      text: markdown(rep?.reportContent),
    }
    const visit = imagingAt.get(result.key)
    doc(
      result.key,
      properties,
      scans.length ? fileDoc(scans[0].f, "imaging", at) : null,
      visit
    )
    scans
      .slice(1)
      .forEach((s, i) =>
        doc(
          s.id,
          { ...properties, name: `${title}, scan ${i + 2}`, text: null },
          fileDoc(s.f, "imaging", at),
          visit
        )
      )
  }

  for (const e of encounters.values()) {
    const lines = [...e.lines]
    if (e.documents.length)
      lines.push("", "Documents:", ...e.documents.map((d) => `- ${d}`))
    records.push(
      record(KINDS.encounter, e.key, {
        ...e.properties,
        text: lines.join("\n"),
      })
    )
  }
  const seen = new Set<string>()
  for (const d of docs)
    if (!seen.has(d.key)) {
      seen.add(d.key)
      records.push(d)
    }

  // ---- health issues and allergies ----
  for (const { stem, value: h } of ex.healthIssues) {
    const item = filled(h.healthIssueItem) ?? filled(h.localItem) ?? {}
    if (!item.name) {
      left.push({
        what: `health issue ${stem.slice(0, 16)}`,
        why: "it has no name",
      })
      continue
    }
    records.push(
      record(KINDS.condition, stem, {
        name: item.name,
        category: "problemlistitem",
        clinicalStatus: "active",
        organization: org,
      })
    )
  }
  for (const { stem, value: a } of ex.allergies) {
    const item = filled(a.allergyItem) ?? filled(a.localItem) ?? {}
    if (!item.name) {
      left.push({ what: `allergy ${stem.slice(0, 16)}`, why: "it has no name" })
      continue
    }
    const reactions = (item.reactionList ?? [])
      .filter((r) => r.title)
      .map((r) => ({ manifestation: r.title }))
    records.push(
      record(KINDS.allergy, stem, {
        name: item.name,
        reactions: reactions.length ? reactions : null,
        organization: org,
      })
    )
  }

  // ---- medicines ----
  const medKeys = numberedKeys(
    ex.medications.map(({ stem, value: m }) => m.id || m.name || stem)
  )
  ex.medications.forEach(({ value: m }, i) => {
    const key = medKeys[i]
    if (!m.name) {
      left.push({ what: `medicine ${key.slice(0, 16)}`, why: "it has no name" })
      return
    }
    const refill = m.refillDetails ?? {}
    const prescribed = !!m.prescriptionNumber
    const prescriber = m.authorizingProvider?.name || null
    const startedAt = displayedDay(m.startDate)
    const request = record(KINDS.medicationrequest, key, {
      medication: m.name,
      dosage: m.sig || null,
      quantity: refill.writtenDispenseQuantity || null,
      // a hospital prescription is one issue, with no repeats
      course: prescribed ? "acute" : null,
      intent: prescribed ? "order" : null,
      status: prescribed ? prescriptionStatus(refill, ctx.today) : "active",
      at: startedAt,
      precision: m.startDate ? "day" : null,
      prescriber,
      organization: prescribed ? org : null,
      notes: prescribed
        ? null
        : "On the hospital's medication list as a medicine prescribed elsewhere; the hospital did not prescribe it.",
    })
    records.push(request)
    const last = refill.lastDispense
    if (!last?.dispenseDate) return
    const when = displayedDay(last.dispenseDate)
    if (!when) {
      left.push({
        what: `the last dispense of ${m.name}`,
        why: "its date is not a date",
      })
      return
    }
    const pharmacy = refill.owningPharmacy?.id
      ? pharmacies.get(refill.owningPharmacy.id)
      : undefined
    records.push(
      record(KINDS.medicationdispense, `${key}/${when.slice(0, 10)}`, {
        medication: m.name,
        quantity: last.dispenseQuantity || null,
        status: "completed",
        at: when,
        issuedAt: startedAt,
        prescription: m.prescriptionNumber || null,
        request: ref(request),
        pharmacy: pharmacy ? ref(pharmacy) : null,
        prescriber,
        organization: org,
      })
    )
  })

  // ---- messages ----
  readMessages(ex, ctx, org, record, keep, records, left)

  return { organizations, records, documents, left }
}

type NoteOf = NonNullable<VisitNotes["noteList"]>[number]

/** An item, where it holds anything. */
function filled<T extends object>(item: T | null | undefined): T | undefined {
  return item && Object.keys(item).length ? item : undefined
}

/** The messages a provider sent, each with the images it embeds: an image
 * every message carries (a banner) or one shaped like a logo is left out. */
function readMessages(
  ex: ClinicalExport,
  ctx: ClinicalContext,
  org: PlannedRef,
  record: (
    kind: string,
    key: string,
    properties: Record<string, unknown>
  ) => PlannedRecord,
  keep: (d: PlannedDocument) => ReturnType<typeof docRef>,
  records: PlannedRecord[],
  left: LeftOut[]
): void {
  interface Sent {
    key: string
    properties: Record<string, unknown>
    images: { file: LoadedFile; digest: string }[]
    at: string | null
  }
  const sent: Sent[] = []
  let own = 0
  for (const { stem, value: c } of ex.conversations) {
    const names = c.userOverrideNames ?? {}
    let n = 0
    for (const msg of c.messages ?? []) {
      const author = msg.author ?? {}
      const refs = (msg.body ?? "").match(IMAGE_REF)?.length ?? 0
      if (author.wprKey) {
        // the owner's own message: the kind holds what a provider sent
        n += refs
        own++
        continue
      }
      const images: Sent["images"] = []
      for (let r = 0; r < refs; r++) {
        n++
        const f = ex.file("messages", `${stem}-${n}`)
        if (f) images.push({ file: f, digest: blobDigest(f.bytes) })
      }
      sent.push({
        key: msg.wmgId,
        at: msg.deliveryInstantISO ?? null,
        images,
        properties: {
          sender:
            (author.empKey ? names[author.empKey] : null) ||
            author.displayName ||
            ctx.hospital.name,
          topic: c.subject || null,
          body: markdown(msg.body),
          at: msg.deliveryInstantISO ?? null,
          organization: org,
        },
      })
    }
  }
  // An image whose bytes repeat in more than half of the messages that
  // carry images (a banner every booking message opens with) is left out.
  const carrying = sent.filter((s) => s.images.length)
  const inMessages = new Map<string, number>()
  for (const s of carrying)
    for (const d of new Set(s.images.map((i) => i.digest)))
      inMessages.set(d, (inMessages.get(d) ?? 0) + 1)
  const repeated = (d: string) => {
    const k = inMessages.get(d) ?? 0
    return k >= 2 && k * 2 > carrying.length
  }
  let dropped = 0
  for (const s of sent) {
    const attachments = []
    for (const i of s.images) {
      if (repeated(i.digest) || decorative(i.file.bytes)) {
        dropped++
        continue
      }
      attachments.push(
        keep(
          plannedDocument(i.file.bytes, {
            provider: PROVIDER,
            kind: "message image",
            date: (s.at && dateIn(s.at, ctx.zone)) || "",
            mediaType: mediaTypeOf(i.file.path),
            ext: extensionOf(i.file.path) || undefined,
          })
        )
      )
    }
    records.push(
      record(KINDS.communication, s.key, {
        ...s.properties,
        attachments: attachments.length ? attachments : null,
      })
    )
  }
  if (own)
    left.push({
      what: `${own} message${own === 1 ? "" : "s"} the owner sent`,
      why: "the communication kind holds what a provider sent",
    })
  if (dropped)
    left.push({
      what: `${dropped} image${dropped === 1 ? "" : "s"} embedded in messages`,
      why: "banners, logos and QR codes: an image repeated across most messages, or shaped like a logo",
    })
}
