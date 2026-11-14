/** How long a kind's history is kept, in words (decision 0282). A value is
 * `all`, `none` or an ISO 8601 duration of weeks, days and a time part; one
 * the reader does not recognise is shown as written. */

import type { KindRetention } from "@/lib/api/types"

const UNITS: Array<[string, string]> = [
  ["W", "week"],
  ["D", "day"],
  ["H", "hour"],
  ["M", "minute"],
  ["S", "second"],
]

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"}`
}

/** One value in words: "all of it", "none past the latest state", "30 days". */
export function retentionText(value: string): string {
  if (value === "all") return "all of it"
  if (value === "none") return "none past the latest state"
  const m =
    /^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/.exec(
      value
    )
  if (!m) return value
  const parts = m
    .slice(1)
    .map((digits, i) =>
      digits ? plural(Number(digits), UNITS[i][1]) : undefined
    )
    .filter((p): p is string => Boolean(p))
  return parts.length ? parts.join(" ") : value
}

/** The line a kind's page shows: what applies, and where an override stands,
 * what the kind declares under it. */
export function retentionLine(r: KindRetention): string {
  const applies = `history kept: ${retentionText(r.effective)}`
  if (!r.override) return applies
  return `${applies} (set in this repository; the kind declares ${retentionText(r.declared ?? "all")})`
}
