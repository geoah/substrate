/** The view's window in a block's words: a record's own span or a view's
 * `last`, which ends now and is said by its length. */

import type { Bound } from "@/lib/record-view"

/** A length in words after "the last": "day", "7 days", "6 hours". */
function lastLength(ms: number): string {
  const units: [number, string][] = [
    [86_400_000, "day"],
    [3_600_000, "hour"],
    [60_000, "minute"],
  ]
  for (const [size, name] of units) {
    if (ms >= size && ms % size === 0) {
      const n = ms / size
      return n === 1 ? name : `${n.toLocaleString("en")} ${name}s`
    }
  }
  const minutes = Math.max(1, Math.round(ms / 60_000))
  return minutes === 1 ? "minute" : `${minutes.toLocaleString("en")} minutes`
}

/** The view's window in a block's words. A record's own span is "this
 * cycle", read "during this cycle" and shown whole as "the whole cycle",
 * and an open one is said "so far"; a view's `last` is its length ending
 * now: "in the last 7 days", "the last 7 days", never "so far". */
export function spanWords(
  bound: Bound | undefined,
  noun: string
): { during: string; whole: string; soFar: string } {
  const w = bound?.window
  if (bound?.lastWindow && w) {
    const last = `the last ${lastLength(w.end - w.start)}`
    return { during: `in ${last}`, whole: last, soFar: "" }
  }
  return {
    during: `during this ${noun}`,
    whole: `the whole ${noun}`,
    soFar: bound?.open && w ? "so far" : "",
  }
}
