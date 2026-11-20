// What the record says of a medicine's issue: a pharmacy's word for an
// electronic prescription item as a dispense's status, an acute issue's
// status from the evidence the export carries, and whether a vaccine issue
// was given (apps/healthspan/docs/importers/05-nhs.md, Medicines).

import { addMonths, strip } from "../shared"

/** The words the Electronic Prescription Service tracks an item by, lower
 * case, as `medicationdispense.status`; any other word is `unknown`. */
export const DISPENSE: Readonly<Record<string, string>> = {
  collected: "completed",
  dispensed: "completed",
  "with dispenser": "preparation",
  "with dispenser - active": "inprogress",
  "to be dispensed": "preparation",
  cancelled: "cancelled",
  "not dispensed": "declined",
  expired: "cancelled",
}

/** A word the app now sends as one (`WithDispenser`, `NotTracked`) and the
 * spaced words the tracker used before. */
const RUN_TOGETHER: Readonly<Record<string, string>> = {
  "with dispenser active": "with dispenser - active",
}

/** An item's status as the tracker's words, lower case: a word sent run
 * together (`WithDispenser`) is split where its capitals start, and
 * `WithDispenserActive` is `with dispenser - active`. */
export function epsWord(status: string | null | undefined): string {
  const s = strip(status ?? "")
  const words = /^[A-Z][a-z]+(?:[A-Z][a-z]+)+$/.test(s)
    ? s.replace(/([a-z])([A-Z])/g, "$1 $2")
    : s
  const lower = words.toLowerCase()
  return RUN_TOGETHER[lower] ?? lower
}

/** An item's status as `medicationdispense.status`. */
export function dispenseStatus(status: string | null | undefined): string {
  return DISPENSE[epsWord(status)] ?? "unknown"
}

/** How long a prescription-only medicine can be dispensed after it is
 * issued: six months (Human Medicines Regulations 2012, reg. 217). */
export const VALIDITY_MONTHS = 6

const STOPPED = new Set(["expired", "not dispensed"])

/** FHIR's `MedicationRequest.status` for one acute issue, issued on
 * `issued` (`YYYY-MM-DD`, or null) and judged on `now`, from the tracker
 * words of the items that match it and whether the record says it was a
 * vaccine given that day. */
export function acuteStatus(
  issued: string | null,
  now: string,
  words: readonly string[],
  administered = false
): "completed" | "cancelled" | "stopped" | "active" | "unknown" {
  const outcome = words.map((w) => DISPENSE[w])
  if (outcome.includes("completed") || administered) return "completed"
  const stopped = words.some((w) => STOPPED.has(w))
  if (outcome.includes("cancelled") && !stopped) return "cancelled"
  if (stopped) return "stopped"
  if (outcome.some((o) => o === "preparation" || o === "inprogress"))
    return "active"
  const until = issued ? addMonths(issued, VALIDITY_MONTHS) : undefined
  if (until && now < until) return "active"
  return "unknown"
}

/** What a vaccine protects against, read from a product's name or an
 * immunisation's term: each antigen with the words and brands that name
 * it. */
export const ANTIGENS: Readonly<Record<string, RegExp>> = {
  typhoid: /typhoid|typhim|vivotif|typherix/,
  "hepatitis a": /hepatitis a\b|avaxim|havrix|vaqta|twinrix|hepatyrix|vivaxim/,
  "hepatitis b": /hepatitis b\b|engerix|fendrix|hbvaxpro|heplisav|twinrix/,
  "covid-19":
    /covid|sars-cov-2|coronavirus|comirnaty|spikevax|vaxzevria|nuvaxovid/,
  influenza: /influenza|\bflu\b|fluenz|fluad|flucelvax|supemtek/,
  tetanus: /tetanus|revaxis|boostrix|repevax/,
  diphtheria: /diphtheria|revaxis|boostrix|repevax/,
  polio: /polio|revaxis|repevax/,
  rabies: /rabies|rabipur|verorab/,
  "tick-borne encephalitis": /tick-borne|ticovac|encepur/,
  cholera: /cholera|dukoral/,
  "japanese encephalitis": /japanese encephalitis|ixiaro/,
  "yellow fever": /yellow fever|stamaril/,
  meningococcal: /meningococcal|menveo|nimenrix|bexsero|trumenba|menquadfi/,
  pneumococcal: /pneumococcal|pneumovax|prevenar|vaxneuvance/,
  shingles: /shingles|herpes zoster|shingrix|zostavax/,
  measles: /measles|\bmmr\b|priorix|mmrvaxpro/,
  hpv: /papilloma|\bhpv\b|gardasil|cervarix/,
  rsv: /respiratory syncytial|\brsv\b|abrysvo|arexvy/,
}

/** A term that says a vaccine was given: "Administration of ...", "...
 * vaccination", "... immunisation". */
const GIVEN = /^(administration of\b|.*\b(vaccination|immuni[sz]ation)$)/
/** A term that says a vaccine is needed, offered, declined or due, which is
 * no evidence it was given. */
const NOT_GIVEN =
  /requires|invitation|invite|declin|refus|contraindicat|not given|\bdue\b|advised|offered|consent|reminder|unsuitable|not required/

/** The antigens a text names. */
export function antigens(text: string | null | undefined): Set<string> {
  const t = (text ?? "").toLowerCase()
  return new Set(
    Object.entries(ANTIGENS)
      .filter(([, pattern]) => pattern.test(t))
      .map(([a]) => a)
  )
}

/** One immunisation of the record: its date (`YYYY-MM-DD`) and term. */
export interface Given {
  date: string
  term: string | null | undefined
}

/** Whether the record says the vaccine `name` names was given on `issued`:
 * an immunisation that day whose term says it was administered and names
 * an antigen the product protects against. */
export function vaccineGiven(
  name: string | null | undefined,
  issued: string | null,
  immunisations: readonly Given[]
): boolean {
  const want = antigens(name)
  if (!want.size || !issued) return false
  return immunisations.some((i) => {
    const t = strip(i.term ?? "").toLowerCase()
    if (i.date !== issued || !GIVEN.test(t) || NOT_GIVEN.test(t)) return false
    return [...antigens(t)].some((a) => want.has(a))
  })
}

/** A medicine's name and another's match where either starts with the
 * other's first 25 characters. */
export function sameMedicine(a: string, b: string): boolean {
  return b.startsWith(first25(a)) || a.startsWith(first25(b))
}

/** The first 25 characters of a name, by code point as Python slices. */
export function first25(name: string): string {
  return Array.from(name).slice(0, 25).join("")
}

/** A `YYYY-MM-DD` date as days since the epoch, or undefined where it is
 * not one. */
export function dayNumber(text: string | null | undefined): number | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(text ?? "")
  if (!m) return undefined
  const ms = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  const d = new Date(ms)
  if (
    d.getUTCFullYear() !== Number(m[1]) ||
    d.getUTCMonth() !== Number(m[2]) - 1 ||
    d.getUTCDate() !== Number(m[3])
  )
    return undefined
  return ms / 86_400_000
}

/** An electronic prescription as matching reads it. */
export interface EpsPrescription {
  date?: string | null
  medications?:
    readonly { name?: string | null; status?: string | null }[] | null
}

/** The tracker words of the items naming `name` on prescriptions dated
 * within `days` of `issued`. */
export function epsMatches(
  name: string | null | undefined,
  issued: string | null,
  prescriptions: readonly EpsPrescription[],
  days = 3
): string[] {
  const out: string[] = []
  const day = dayNumber(issued)
  for (const p of prescriptions) {
    const when = dayNumber((p.date ?? "").slice(0, 10))
    if (when === undefined || day === undefined) continue
    if (Math.abs(when - day) > days) continue
    for (const item of p.medications ?? []) {
      const n = item.name ?? ""
      if (name && n && sameMedicine(name, n)) out.push(epsWord(item.status))
    }
  }
  return out
}
