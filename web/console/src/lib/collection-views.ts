/** A collection's shipped saved views (decision record 0229): records of
 * `substrate.reamde.dev/console/collectionview` a package ships and the
 * owner may edit, shown as tabs after All and before the owner's own views.
 *
 * A record holds its filter in the API's grammar (`where`, one condition per
 * property) and names properties everywhere, never this console's filter
 * tokens or column ids. This module converts both ways: a record into the
 * SavedView the strip compares, and the page's state back into the
 * properties a "save changes" writes. A condition the filter bar has no
 * control for leaves the view listed with the reason, never shown as
 * something it is not. Nothing here names a kind. */

import { CORE_AUTHORITY } from "@/lib/core-kinds"
import type { FirstPageParams } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import {
  decodeFilters,
  encodeFilter,
  toRecordFilter,
  type ActiveFilter,
} from "@/lib/filters"
import { isMoney, moneyText } from "@/lib/money"
import { kindDeclarationPath } from "@/lib/record-view"
import type { SavedView, ViewState } from "@/lib/saved-views"
import type { UnitReader } from "@/lib/unit-reader"

/** The kind a shipped saved view is a record of. */
export const COLLECTION_VIEW_KIND = `${CORE_AUTHORITY}/console/collectionview`

/** The list read of the views shipped for one collection: one page as large
 * as the server serves, since the list cannot be ordered by id and the
 * strip orders the views itself. */
export function collectionViewsParams(identity: string): FirstPageParams {
  return {
    kinds: [COLLECTION_VIEW_KIND],
    first: 500,
    filter: {
      properties: { appliesTo: { eq: kindDeclarationPath(identity) } },
    },
  }
}

/** The strip's id for a shipped view: its record path, which no view kept on
 * the preference record can hold (those are eight letters and digits). */
export function collectionViewId(recordId: string): string {
  return `${COLLECTION_VIEW_KIND}/${recordId}`
}

const COMPARATOR = { lt: "<", lte: "<=", gte: ">=", gt: ">" } as const

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v)

/** An operand as the bar types it, which the bar reads back to the same
 * operand: a scalar, a money value (`19.99 EUR`) or a quantity (`155 lb`) on
 * a property of that type. Undefined for anything else. A comma is
 * membership in the bar, so a text value holding one cannot be an exact
 * value there. */
function operandText(
  v: unknown,
  prop: DeclaredProperty | undefined
): string | undefined {
  if (typeof v === "boolean" || typeof v === "number") return String(v)
  if (typeof v === "string") return v.includes(",") ? undefined : v
  if (prop?.kind === "money" && isMoney(v)) return moneyText(v)
  if (prop?.kind === "quantity" && isObject(v)) {
    const { value, unit, ...rest } = v
    if (
      (typeof value === "string" || typeof value === "number") &&
      typeof unit === "string" &&
      Object.keys(rest).length === 0
    )
      return `${value} ${unit}`
  }
  return undefined
}

/** One property's condition as filter rows, or why the bar cannot show it. */
function condRows(
  field: string,
  cond: unknown,
  prop: DeclaredProperty | undefined
): ActiveFilter[] | string {
  if (!isObject(cond) || Object.keys(cond).length === 0)
    return `its condition on ${field} is not an object of filter operators`
  const rows: ActiveFilter[] = []
  const cannot = (op: string) =>
    `the filter bar cannot show its “${op}” condition on ${field}`
  for (const [op, operand] of Object.entries(cond)) {
    switch (op) {
      case "eq":
      case "contains": {
        const value = operandText(operand, prop)
        if (value === undefined) return cannot(op)
        rows.push({ field, op, value })
        break
      }
      case "in": {
        const values = Array.isArray(operand)
          ? operand.map((v) => operandText(v, prop))
          : []
        if (!values.length || values.some((v) => v === undefined))
          return cannot(op)
        rows.push({ field, op: "eq", value: values.join(",") })
        break
      }
      case "prefix":
      case "match":
        if (typeof operand !== "string" || !operand) return cannot(op)
        rows.push({ field, op, value: operand })
        break
      case "exists":
        if (typeof operand !== "boolean") return cannot(op)
        rows.push({ field, op: "exists", value: String(operand) })
        break
      case "lt":
      case "lte":
      case "gte":
      case "gt": {
        // The bar types a bound on a quantity alone, as `<155 lb`.
        const quantity =
          prop?.kind === "quantity" && !prop.repeated && isObject(operand)
            ? operand
            : undefined
        const value = quantity?.value
        if (
          !quantity ||
          (typeof value !== "string" && typeof value !== "number") ||
          typeof quantity.unit !== "string"
        )
          return cannot(op)
        rows.push({
          field,
          op: "eq",
          value: `${COMPARATOR[op]}${value} ${quantity.unit}`,
        })
        break
      }
      default:
        return cannot(op)
    }
  }
  return rows
}

/** A view's `where` as the bar's filter tokens, in property order, or why
 * the bar cannot show it. Absent is no filter. */
export function whereTokens(
  where: unknown,
  props: readonly DeclaredProperty[]
): string[] | { problem: string } {
  if (where === undefined || where === null) return []
  if (!isObject(where))
    return { problem: "its filter is not one condition per property" }
  const tokens: string[] = []
  for (const field of Object.keys(where).sort()) {
    const rows = condRows(
      field,
      where[field],
      props.find((p) => p.name === field)
    )
    if (typeof rows === "string") return { problem: rows }
    tokens.push(...rows.map(encodeFilter))
  }
  return tokens
}

/** The bar's filter tokens as a view's `where`, the wire's grammar exactly
 * as the page reads it; undefined for no filter. */
export function tokensWhere(
  units: UnitReader,
  tokens: readonly string[],
  props: DeclaredProperty[]
): Record<string, unknown> | undefined {
  return toRecordFilter(units, decodeFilters([...tokens]), props)?.properties
}

const stringList = (v: unknown): string[] | undefined =>
  Array.isArray(v) && v.every((x) => typeof x === "string") ? v : undefined

const text = (v: unknown): string | undefined =>
  typeof v === "string" && v.trim() ? v : undefined

/** How the page names its columns: the column id a property's column has,
 * and every column id the collection draws. */
export interface ColumnNames {
  idOf: (property: string) => string
  all: readonly string[]
}

/** The columns a view's list never hides: they belong to no property. */
const UNHIDDEN = new Set(["title", "id"])

/** One shipped view record as the strip's SavedView. `columns` names the
 * columns the view shows, in order, so every other column the collection
 * draws but the title and the technical id is hidden. */
export function collectionViewOf(
  record: SubstrateRecord,
  collection: string,
  props: readonly DeclaredProperty[],
  columns: ColumnNames
): SavedView {
  const p = record.properties
  const view: SavedView = {
    id: collectionViewId(record.id),
    collection,
    name: text(p.name) ?? record.id,
    record: record.id,
  }
  if (p.default === true) view.isDefault = true
  const tokens = whereTokens(p.where, props)
  if (Array.isArray(tokens)) {
    if (tokens.length) view.filter = tokens
  } else {
    view.problem = tokens.problem
  }
  const sort = text(p.sort)
  if (sort) view.sort = sort
  const shown = stringList(p.columns)
  if (shown?.length) {
    const ids = shown.map(columns.idOf).filter((id) => columns.all.includes(id))
    view.columns = ids
    view.hidden = columns.all.filter(
      (id) => !UNHIDDEN.has(id) && !ids.includes(id)
    )
  }
  const group = text(p.group)
  if (group) view.group = group
  if (p.nest === false) view.nest = false
  return view
}

/** A collection's shipped views, in record id order: the ids' code units,
 * as the server compares them, never the reader's locale. */
export function collectionViewsOf(
  records: readonly SubstrateRecord[],
  collection: string,
  props: readonly DeclaredProperty[],
  columns: ColumnNames
): SavedView[] {
  return [...records]
    .filter((r) => !r.deletedAt)
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((r) => collectionViewOf(r, collection, props, columns))
}

/** The shipped view a bare address opens on: the first marked default. */
export function defaultView(
  views: readonly SavedView[]
): SavedView | undefined {
  return views.find((v) => v.record && v.isDefault && !v.problem)
}

/** The page's state as a shipped view's properties: what "save changes"
 * writes. A dimension at the collection's default is written as null, which
 * deletes the property, since a write merges. `propertyOf` names the
 * property a column shows, undefined for one that is no property's (the
 * title, the id). */
export function collectionViewWrite(
  state: ViewState,
  defaultSort: string,
  where: Record<string, unknown> | undefined,
  propertyOf: (columnId: string) => string | undefined
): Record<string, unknown> {
  const shown = state.columns
    .filter((id) => !state.hidden.includes(id))
    .map(propertyOf)
    .filter((name): name is string => Boolean(name))
  return {
    where: where ?? null,
    sort: state.sort === defaultSort ? null : state.sort,
    columns: shown.length ? shown : null,
    group: state.group ?? null,
    nest: state.nest ? null : false,
  }
}
