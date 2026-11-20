// A finding: one thing that needs attention, from any section's rules.
// `danger` for a lab flag or a red signal, `warning` for a target missed,
// `info` for stale or missing data. `word` is the status word shown beside
// the tone: never colour alone.

export type FindingSection = "labs" | "healthspan" | "record"

export type FindingTone = "danger" | "warning" | "info"

export interface Finding {
  section: FindingSection
  /** What the finding is about, unique within its section (a series key). */
  subject: string
  title: string
  detail: string
  tone: FindingTone
  word: string
  /** The reading or record the finding is dated by, ISO 8601. */
  since?: string
  /** Where it opens, a route's hash (`#/labs/ldl`). */
  href: string
  /** Lower sorts first: the rule's number within its section. */
  rank: number
  important?: boolean
  /** How far outside its band, in band widths; larger sorts first. */
  distance?: number
}

export function sortFindings(fs: readonly Finding[]): Finding[] {
  return [...fs].sort(
    (a, b) =>
      a.rank - b.rank ||
      Number(!!b.important) - Number(!!a.important) ||
      (b.distance ?? 0) - (a.distance ?? 0) ||
      a.title.localeCompare(b.title)
  )
}
