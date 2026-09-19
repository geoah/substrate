/** The shape of a numbered pagination bar: which page numbers it draws and
 * where it elides. Its own file because the bar that renders it
 * (components/data-table/data-table-pagination.tsx) exports a component and
 * nothing else. */

/** How many numbered buttons the bar draws at most, ellipses aside. Seven is
 * the first and last, the current one, its two neighbours, and two gaps. */
const WINDOW = 7

/** The page numbers to draw, `null` for an elided run. The first and the last
 * page are always present, so either end is always one click away, and the
 * middle run is the current page with one neighbour either side — pushed off
 * whichever end it would overhang, so the bar keeps a constant width as the
 * reader walks. */
export function pageItems(page: number, pageCount: number): (number | null)[] {
  if (pageCount <= WINDOW) {
    return Array.from({ length: pageCount }, (_, i) => i + 1)
  }
  let from = Math.max(page - 1, 2)
  let to = Math.min(page + 1, pageCount - 1)
  // At either end one gap disappears, so the run grows by one to take its
  // place: without this the bar is a button narrower on page 1 than page 5,
  // and every number under the pointer moves as the reader walks in.
  if (from <= 3) {
    from = 2
    to = 5
  } else if (to >= pageCount - 2) {
    to = pageCount - 1
    from = pageCount - 4
  }
  const items: (number | null)[] = [1]
  if (from > 2) items.push(null)
  for (let n = from; n <= to; n++) items.push(n)
  if (to < pageCount - 1) items.push(null)
  items.push(pageCount)
  return items
}
