// A printout's text by position (apps/healthspan/docs/importers/04-dxa.md,
// A printout's text): pdf.js's items on one baseline are a row of cells,
// a field is the text right of its label on the label's row, and a table is
// found by its header row, each cell of a row below it read as the column
// whose header label it lies under.

import { strip, type PdfPage } from "../shared"

/** One item of text where the page puts it, from its left edge to its
 * right. */
export interface Cell {
  text: string
  left: number
  right: number
}

/** The cells on one baseline, left to right. */
export interface Row {
  y: number
  cells: Cell[]
}

/** A page's rows, the top first: items whose baselines lie within
 * `tolerance` of a row's first item are that row (the rule the shared
 * `pageLines` keeps), each item a cell, text stripped and its inner runs of
 * whitespace one space, items holding only whitespace left out. */
export function pageRows(page: PdfPage, tolerance = 2): Row[] {
  const items = page.items
    .filter((i) => strip(i.text))
    .sort((a, b) => b.y - a.y || a.x - b.x)
  const rows: Row[] = []
  for (const item of items) {
    const cell: Cell = {
      text: strip(item.text).replace(/\s+/g, " "),
      left: item.x,
      right: item.x + item.width,
    }
    const row = rows.find((r) => Math.abs(r.y - item.y) <= tolerance)
    if (row) row.cells.push(cell)
    else rows.push({ y: item.y, cells: [cell] })
  }
  for (const r of rows) r.cells.sort((a, b) => a.left - b.left)
  return rows
}

/** The text that follows `label` on its row (the rest of the label's own
 * cell, then the cells right of it, one space apart), from the first row
 * holding a cell that starts with it; undefined where none does. */
export function fieldAfter(
  rows: readonly Row[],
  label: string
): string | undefined {
  for (const row of rows) {
    const i = row.cells.findIndex((c) => c.text.startsWith(label))
    if (i < 0) continue
    return [
      row.cells[i].text.slice(label.length),
      ...row.cells.slice(i + 1).map((c) => c.text),
    ]
      .join(" ")
      .replace(/\s+/g, " ")
      .trim()
  }
  return undefined
}

/** A column a table is read by: the header label it lies under (any of
 * its spellings). */
export interface ColumnSpec<K extends string> {
  key: K
  labels: readonly string[]
  /** A column the header can leave out (a printout with no reference
   * population prints no T-score). */
  optional?: boolean
}

/** A table's header row and its rows, as found on one page. */
export interface Table<K extends string> {
  /** 1 for the page's first. */
  page: number
  header: Row
  /** Each column's header label, where the header holds it. */
  columns: Partial<Record<K, Cell>>
  rows: Row[]
}

/** How far two spans overlap, 0 where they do not. */
function overlap(a: Cell, b: Cell): number {
  return Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left))
}

/** The columns a header row holds, in order: each spec's label matched to
 * the next cell after the one before it; undefined where a required
 * column's label is not there. */
function headerColumns<K extends string>(
  row: Row,
  spec: readonly ColumnSpec<K>[]
): Partial<Record<K, Cell>> | undefined {
  const out: Partial<Record<K, Cell>> = {}
  let from = 0
  for (const col of spec) {
    const i = row.cells.findIndex(
      (c, j) => j >= from && col.labels.includes(c.text)
    )
    if (i < 0) {
      if (col.optional) continue
      return undefined
    }
    out[col.key] = row.cells[i]
    from = i + 1
  }
  return out
}

/** A cell's words, each a cell of its own where it lies: pdf.js joins two
 * values printed close together into one item, a space between, and each
 * word is given the share of the item's width its characters take. */
export function words(cell: Cell): Cell[] {
  const chars = [...cell.text]
  if (!chars.includes(" ")) return [cell]
  const each = (cell.right - cell.left) / chars.length
  const out: Cell[] = []
  let at = 0
  for (const word of cell.text.split(" ")) {
    const n = [...word].length
    if (n)
      out.push({
        text: word,
        left: cell.left + at * each,
        right: cell.left + (at + n) * each,
      })
    at += n + 1
  }
  return out
}

/** The words of `row` under a header cell: those overlapping it more than
 * any other cell of the header row, one space apart; undefined where none
 * does. */
export function under(
  row: Row,
  header: Row,
  column: Cell | undefined
): string | undefined {
  if (!column) return undefined
  const texts = row.cells
    .flatMap(words)
    .filter((c) => {
      const mine = overlap(c, column)
      return (
        mine > 0 &&
        header.cells.every((h) => h === column || overlap(c, h) <= mine)
      )
    })
    .map((c) => c.text)
  return texts.length ? texts.join(" ") : undefined
}

/** The words of `row` wholly left of a header cell, one space apart: a
 * region's label, left of a table's first value. */
export function leftOf(row: Row, column: Cell | undefined): string {
  if (!column) return ""
  return row.cells
    .flatMap(words)
    .filter((c) => c.right <= column.left)
    .map((c) => c.text)
    .join(" ")
}

/** How far below its header a table's first row may start: the header's
 * own further lines (its units) lie between. */
const HEADER_DEPTH = 40

/** Every table on a page whose header row holds the columns' labels, left
 * to right. Its rows are the rows below the header that `isRow` takes, from
 * the first (within a few lines of the header, whose own further lines are
 * passed over) to the last before one it does not take. */
export function findTables<K extends string>(
  rows: readonly Row[],
  page: number,
  spec: readonly ColumnSpec<K>[],
  isRow: (row: Row, table: Table<K>) => boolean
): Table<K>[] {
  const out: Table<K>[] = []
  for (let i = 0; i < rows.length; i++) {
    const columns = headerColumns(rows[i], spec)
    if (!columns) continue
    const table: Table<K> = { page, header: rows[i], columns, rows: [] }
    for (let j = i + 1; j < rows.length; j++) {
      const row = rows[j]
      if (isRow(row, table)) table.rows.push(row)
      else if (table.rows.length || rows[i].y - row.y > HEADER_DEPTH) break
    }
    out.push(table)
  }
  return out
}

/** A table row's cell under a column. */
export function cellOf<K extends string>(
  table: Table<K>,
  row: Row,
  key: K
): string | undefined {
  return under(row, table.header, table.columns[key])
}
