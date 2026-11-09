// What a clinical record's importers share (apps/healthspan/docs/importers/
// 00-shared.md, Clinical records): a code from the GP system's id and term,
// an address, a name in capitals made readable, a date's precision, the
// health service's identifiers, and an organization, which every importer
// that meets it merges into one record.

import type { PlannedRecord } from "../types"
import { strip } from "./numbers"
import { organizationKey } from "./keys"
import { SNOMED } from "./ranges"

export { SNOMED } from "./ranges"

/** The system an English organization's ODS code belongs to. */
export const ODS_SYSTEM = "https://fhir.nhs.uk/Id/ods-organization-code"

/** The system an NHS number belongs to. */
export const NHS_NUMBER_SYSTEM = "https://fhir.nhs.uk/Id/nhs-number"

/** The system for a code the GP system gives that is not SNOMED CT's. */
export const EMIS_SYSTEM = "emis"

export interface Coding {
  system: string
  code?: string
  descriptionId?: string
  display?: string
}

/** A GP system's code id and term as a coding: an id whose partition digits
 * (the second and third from the end) are `01` or `11` is a SNOMED CT
 * description id, `00` or `10` a SNOMED CT concept, anything else the GP
 * system's own code. Null for no id. */
export function emisCoding(
  codeId: string | number | null | undefined,
  term?: string | null
): Coding | null {
  if (codeId === null || codeId === undefined || codeId === "" || codeId === 0)
    return null
  const id = String(codeId)
  const partition = id.slice(-3, -1)
  const display = term ?? undefined
  const out: Coding =
    partition === "01" || partition === "11"
      ? { system: SNOMED, descriptionId: id }
      : partition === "00" || partition === "10"
        ? { system: SNOMED, code: id }
        : { system: EMIS_SYSTEM, code: id }
  if (display !== undefined) out.display = display
  return out
}

export interface Address {
  lines?: string[]
  city?: string
  postalCode?: string
  country?: string
}

/** An address from its lines, town and postcode: each stripped, the empty
 * ones left out; null where nothing is left. */
export function address(
  lines: readonly (string | null | undefined)[],
  city?: string | null,
  postalCode?: string | null,
  country?: string | null
): Address | null {
  const out: Address = {}
  const kept = lines.map((l) => strip(l ?? "")).filter(Boolean)
  if (kept.length) out.lines = kept
  const c = strip(city ?? "")
  if (c) out.city = c
  const p = strip(postalCode ?? "")
  if (p) out.postalCode = p
  const k = strip(country ?? "")
  if (k) out.country = k
  return Object.keys(out).length ? out : null
}

/** The words an all-capitals name keeps in capitals. */
export const KEEP_CAPITALS: ReadonlySet<string> = new Set([
  "NHS",
  "GP",
  "UK",
  "CCG",
  "ICB",
  "PCN",
  "HCA",
])

function allCapitals(s: string): boolean {
  return s === s.toUpperCase() && s !== s.toLowerCase()
}

/** A name a health service wrote in capitals, readable: each word with its
 * first letter in capitals and the rest lower (`THE EXAMPLE SURGERY` is
 * `The Example Surgery`), the abbreviations in `KEEP_CAPITALS` kept. A
 * name with any lower-case letter is kept as written. */
export function tidyName(name: string): string {
  if (!allCapitals(name)) return name
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((w) =>
      KEEP_CAPITALS.has(w)
        ? w
        : w.charAt(0).toUpperCase() + w.slice(1).toLowerCase()
    )
    .join(" ")
}

/** How precise a date is, from the GP system's `datePart`. */
export function precision(
  datePart: string | null | undefined
): "year" | "month" | "day" | "time" | null {
  switch (datePart) {
    case "Year":
      return "year"
    case "YearMonth":
      return "month"
    case "YearMonthDay":
      return "day"
    case "YearMonthDayTime":
      return "time"
    default:
      return null
  }
}

/** A stamp's date as midnight UTC (`2026-01-02T00:00:00Z`), null where
 * there is none. */
export function atDate(stamp: string | null | undefined): string | null {
  return stamp ? `${stamp.slice(0, 10)}T00:00:00Z` : null
}

/** Text with the invisible marks some portals wrap a phone number in
 * (U+200B, U+202A to U+202E) removed, stripped, null where empty. */
export function cleanText(text: string | null | undefined): string | null {
  const s = strip((text ?? "").replace(/[\u200b\u202a-\u202e]/g, ""))
  return s || null
}

/** A phone number as written, stripped; null where empty. */
export function phone(text: string | null | undefined): string | null {
  return cleanText(text)
}

/** A web address with `https://` before it where it names no scheme; null
 * where empty. */
export function absoluteUrl(url: string | null | undefined): string | null {
  const u = strip(url ?? "")
  if (!u) return null
  return /^[a-z][a-z0-9+.-]*:/i.test(u) ? u : `https://${u}`
}

/** The first non-blank line of a text, its tags taken for spaces, cut to
 * `max` characters with an ellipsis; null where there is none. */
export function firstLine(
  text: string | null | undefined,
  max = 80
): string | null {
  const plain = (text ?? "").replace(/<[^>]*>/g, " ")
  const line = plain
    .split(/[\r\n]+/)
    .map((l) => strip(l))
    .find(Boolean)
  if (!line) return null
  return line.length > max ? `${line.slice(0, max - 1).trimEnd()}…` : line
}

export interface Identifier {
  system?: string
  value: string
  label?: string
}

/** An organization's ODS code as an identifier. */
export function odsIdentifier(code: string): Identifier {
  return { system: ODS_SYSTEM, value: code, label: "ODS code" }
}

/** An NHS number as an identifier, its spaces removed. */
export function nhsNumberIdentifier(nhsNumber: string): Identifier {
  return {
    system: NHS_NUMBER_SYSTEM,
    value: nhsNumber.replace(/\s+/g, ""),
    label: "NHS number",
  }
}

export type OrganizationType =
  | "gppractice"
  | "hospital"
  | "pharmacy"
  | "laboratory"
  | "clinic"
  | "dental"
  | "optical"
  | "community"
  | "other"

/** An organization as an importer knows it. Only what is given is written:
 * an importer that knows no address leaves the key out, never clearing one
 * another importer wrote. */
export interface OrganizationInput {
  ods: string
  name: string
  type?: OrganizationType
  address?: Address | null
  phone?: string | null
  url?: string | null
  /** Identifiers besides the ODS code (an Epic id). */
  identifiers?: readonly Identifier[]
}

/** An organization's planned record, `ods/<code>`, merged into what any
 * importer wrote there: identifiers unioned, every other field filled only
 * where empty. */
export function organizationRecord(
  o: OrganizationInput,
  revision: number
): PlannedRecord {
  const properties: Record<string, unknown> = {
    name: o.name,
    identifiers: [odsIdentifier(o.ods), ...(o.identifiers ?? [])],
  }
  if (o.type) properties.type = o.type
  if (o.address) properties.address = o.address
  if (o.phone) properties.phone = o.phone
  if (o.url) properties.url = o.url
  return { ...organizationKey(o.ods), mode: "merge", properties, revision }
}
