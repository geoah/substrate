/** The numbered window: 1 … around current … last, ellipses between runs.
 * Pure page math, shared with the pagination piece and its tests. */
export function pageItems(
  current: number,
  pageCount: number
): (number | "…")[] {
  if (pageCount <= 7) {
    return Array.from({ length: pageCount }, (_, i) => i + 1)
  }
  const pages = new Set<number>([1, pageCount])
  for (let p = current - 1; p <= current + 1; p++) {
    if (p >= 1 && p <= pageCount) pages.add(p)
  }
  const sorted = [...pages].sort((a, b) => a - b)
  const out: (number | "…")[] = []
  let prev = 0
  for (const p of sorted) {
    if (prev && p - prev > 1) out.push("…")
    out.push(p)
    prev = p
  }
  return out
}
