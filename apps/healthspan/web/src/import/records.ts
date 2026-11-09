// The keys of the records a CSV or a hand entry plans, in the import's own
// namespace; the kinds an import may create; and how a record found at one
// of a file's ids is compared with what the plan writes there. The records
// themselves are built in plan.ts, which loads with the import screen.

import type { SubstrateRecord } from "@substrate/client"
import { KIND } from "../kinds"
import { unitSpellingKey } from "../labs/units"
import { OWN } from "./ids"
import type { RecordKey } from "./importers/types"

/** The HL7 code system a lab's H, L, A and N flags belong to. */
export const INTERPRETATION =
  "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"

/** The lab's source a CSV or a hand entry plans: the plan's `source`, which
 * no record of the plan writes. Setup decides it by the lab's name, and
 * every reference to it is to the source the import then names. */
export const LAB_SOURCE: RecordKey = {
  kind: KIND.source,
  namespace: OWN,
  key: "lab",
}

export const reportKey = (key: string): RecordKey => ({
  kind: KIND.labreport,
  namespace: OWN,
  key,
})

export const resultKey = (key: string): RecordKey => ({
  kind: KIND.labresult,
  namespace: OWN,
  key,
})

const LAB_KINDS: readonly string[] = [KIND.labresult, KIND.labreport]

/** The clinical kinds an import may create; an organization carries no
 * `origin`. */
const CLINICAL_KINDS: readonly string[] = [
  "patient",
  "encounter",
  "condition",
  "allergy",
  "immunization",
  "medicationrequest",
  "medicationdispense",
  "appointment",
  "referral",
  "document",
  "communication",
].map((k) => `substrate.reamde.dev/clinical/${k}`)

/** The kinds whose records may point at an import of an importer through
 * `origin`: what it may create. A source and an organization carry none. */
export function originKinds(importer: string | undefined): string[] {
  return importer === "nhs" || importer === "mychart"
    ? [...LAB_KINDS, ...CLINICAL_KINDS]
    : [...LAB_KINDS]
}

/** A reference as served (`{ref}`) or written (the path). */
export function refPath(v: unknown): string | undefined {
  if (typeof v === "string") return v
  if (v && typeof v === "object" && "ref" in v) {
    const r = v.ref
    return typeof r === "string" ? r : undefined
  }
  return undefined
}

function sameInstant(a: unknown, b: unknown): boolean {
  return (
    typeof a === "string" &&
    typeof b === "string" &&
    Date.parse(a) === Date.parse(b)
  )
}

/** A unit's identity: the names one unit goes by (its id, its code, an
 * alias) give one key. */
export type UnitKey = (name: string) => string

const spelled: UnitKey = unitSpellingKey

const UNIT_PREFIX = `${KIND.unit}/`

/** A quantity's unit as written or served: its id or spelling, a reference
 * to the unit record read as its id. */
export function unitName(v: unknown): string | undefined {
  const path = refPath(v)
  if (!path) return undefined
  return path.startsWith(UNIT_PREFIX) ? path.slice(UNIT_PREFIX.length) : path
}

function sameQuantity(a: unknown, b: unknown, unitKey: UnitKey): boolean {
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return a === b
  const x = a as { value?: unknown; comparator?: unknown; unit?: unknown }
  const y = b as { value?: unknown; comparator?: unknown; unit?: unknown }
  const nx = unitName(x.unit)
  const ny = unitName(y.unit)
  const ux = nx === undefined ? undefined : unitKey(nx)
  const uy = ny === undefined ? undefined : unitKey(ny)
  return (
    ux === uy &&
    Number(x.value) === Number(y.value) &&
    (x.comparator ?? undefined) === (y.comparator ?? undefined)
  )
}

/** Whether a row found at an id the plan writes is what the plan writes
 * there: the same import, metric or report, time, and value or words. The
 * server stores a unit under its id and keeps the digits as written, so a
 * value is the same number in the same unit, the unit compared through
 * `unitKey` (`%` and `percent` are one unit; `g/dL` and `g/L` are not). */
export function sameAsPlanned(
  kind: string,
  sent: Record<string, unknown>,
  found: SubstrateRecord,
  unitKey: UnitKey = spelled
): boolean {
  const p = found.properties
  if (refPath(p.origin) !== sent.origin) return false
  if (!sameInstant(p.at, sent.at)) return false
  if (kind === KIND.labreport)
    return (p.accession ?? undefined) === (sent.accession ?? undefined)
  if (refPath(p.metric) !== sent.metric) return false
  if (refPath(p.report) !== sent.report) return false
  if (sent.text !== undefined) return p.text === sent.text
  return sameQuantity(p.value, sent.value, unitKey)
}
