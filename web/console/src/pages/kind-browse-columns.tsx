/* eslint-disable react-refresh/only-export-components -- a columns.tsx is a
 * factory of cell renderers, not a page module; nothing here hot-reloads on
 * its own (the official DataTable pattern's columns file has this shape). */

/** The per-kind column factory (the official pattern's `columns.tsx`, made
 * schema-driven because ONE generic table covers every kind): `title` +
 * temporal always, declared properties after in schema casing with record-56
 * descriptions on the headers, `updated` on the right. Rule 6: the first and last columns carry the page gutter. */

import type { DataTableColumn } from "@/components/data-table/data-table"

import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { StateBadge } from "@/components/state-badge"
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  cellValue,
  recordTitle,
  referenceCell,
  relativeTime,
  tableDateTime,
} from "@/lib/format"
import {
  columnProperties,
  temporalProperties,
  type DeclaredProperty,
} from "@/lib/definition"

/** Wire names the engine reserves as hot/system columns (recordColumns in
 * engine/query.go). A DECLARED property sharing one of these names cannot be
 * wire-sorted independently — orderBy would hit the system column — so such
 * columns render unsorted and their ids are namespaced apart. */
const RESERVED_SORT_NAMES = new Set([
  "title",
  "body",
  "at",
  "endsAt",
  "dueAt",
  "createdAt",
  "updatedAt",
  "deletedAt",
  "id",
  "kind",
  "version",
])

/** Declared property column id (`prop:name`), kept distinct from the system
 * columns so a provider property named `updatedAt` cannot collide. */
export function propertyColumnId(name: string): string {
  return `prop:${name}`
}

/** Column id → the wire property `orderBy` speaks. */
export function sortPropertyOf(columnId: string): string {
  return columnId.startsWith("prop:") ? columnId.slice(5) : columnId
}

/** Wire property (from the URL) → the column id carrying its indicator. The
 * system columns own the reserved names; everything else is a declared
 * property column. */
export function columnIdOf(property: string): string {
  return RESERVED_SORT_NAMES.has(property)
    ? property
    : propertyColumnId(property)
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>
}

function propertyCell(prop: DeclaredProperty, value: unknown) {
  if (value === undefined || value === null) return <Muted>—</Muted>
  if (prop.kind === "state") {
    return <StateBadge value={String(value)} initial={prop.initial} />
  }
  if (prop.kind === "bool") {
    return <span className="data">{String(value)}</span>
  }
  // Declared datetimes read local like the hot columns; hover keeps the wire
  // ISO. A bare `date` kind stays verbatim — it has no instant to localize.
  if (prop.kind === "datetime" && typeof value === "string") {
    return (
      <span className="data text-muted-foreground" title={value}>
        {tableDateTime(value)}
      </span>
    )
  }
  // A reference's stored value is the referent's whole path; the column already
  // says which kind it points at, so the cell names the record.
  const text =
    prop.kind === "reference" ? referenceCell(value) : cellValue(value)
  if (!text) return <Muted>—</Muted>
  // The cell uses its column's whole width; truncation happens only at the
  // column boundary, full value on hover (owner ruling, 2026-08-06 — no
  // arbitrary max-w clamps).
  return (
    <span className="block truncate data text-foreground/80" title={text}>
      {text}
    </span>
  )
}

/** THE kind → size table (owner-approved column-sizing fix, 2026-08-06).
 * Content-hugging kinds get a fixed px; everything prose-ish declares
 * `{min, max?, weight}` and shares the container proportionally
 * (lib/column-widths.ts) instead of every property flat-rating 150px:
 *
 *   state              fixed 120   (badge hugs its longest state)
 *   bool               fixed 80    (true/false)
 *   datetime / date    fixed 150   (`Aug 6, 01:10` never grows)
 *   int / float        min 90  max 140  weight 0.5
 *   email / url / phone min 160 max 280 weight 1
 *   string (+ custom)  min 160 max 420 weight 1.5
 *   text / markdown    min 200 max 480 weight 2 (excluded from columns
 *                      today — LONG_KINDS — but the vocabulary is complete)
 */
type PropertySizing = Pick<
  NonNullable<DataTableColumn<SubstrateRecord>["meta"]>,
  "width" | "size"
>

function propertySizing(prop: DeclaredProperty): PropertySizing {
  if (prop.kind === "state") return { width: 120 }
  if (prop.kind === "bool") return { width: 80 }
  if (prop.kind === "datetime" || prop.kind === "date") return { width: 150 }
  if (prop.kind === "int" || prop.kind === "float" || prop.kind === "decimal") {
    return { size: { min: 90, max: 140, weight: 0.5 } }
  }
  if (prop.kind === "email" || prop.kind === "url" || prop.kind === "phone") {
    return { size: { min: 160, max: 280, weight: 1 } }
  }
  if (prop.kind === "text" || prop.kind === "markdown") {
    return { size: { min: 200, max: 480, weight: 2 } }
  }
  // string, and authority-local datatypes treated as strings.
  return { size: { min: 160, max: 420, weight: 1.5 } }
}

export function buildColumns(
  kind: KindInfo
): DataTableColumn<SubstrateRecord>[] {
  const columns: DataTableColumn<SubstrateRecord>[] = []

  // title — always first, always sortable, never hidden (the row's identity).
  columns.push({
    id: "title",
    accessorFn: (e) => recordTitle(e.properties),
    enableHiding: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="title" />
    ),
    cell: ({ row }) => {
      const title = recordTitle(row.original.properties)
      return title ? (
        <span className="block truncate font-medium" title={title}>
          {title}
        </span>
      ) : (
        <span
          className="block truncate data text-muted-foreground"
          title={row.original.id}
        >
          {row.original.id}
        </span>
      )
    },
    // the row's identity earns the biggest share, but capped — a title
    // column must not balloon across a wide screen while data truncates.
    meta: { label: "title", size: { min: 180, max: 460, weight: 2 } },
  })

  // temporal — the trait-bound hot columns, right after title.
  for (const name of temporalProperties(kind)) {
    columns.push({
      id: name,
      accessorFn: (e) => e.properties[name],
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={name} />
      ),
      cell: ({ getValue }) => {
        const value = getValue()
        // local-timezone stamp; the title carries the wire ISO verbatim.
        return typeof value === "string" ? (
          <span className="data text-muted-foreground" title={value}>
            {tableDateTime(value)}
          </span>
        ) : (
          <Muted>—</Muted>
        )
      },
      meta: { label: name, width: 150 },
    })
  }

  // declared properties, schema casing, description tooltips on headers,
  // widths by declared kind (the table above).
  for (const prop of columnProperties(kind)) {
    columns.push({
      id: propertyColumnId(prop.name),
      accessorFn: (e) => e.properties[prop.name],
      enableSorting: !prop.repeated && !RESERVED_SORT_NAMES.has(prop.name),
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={prop.name}
          description={prop.description}
        />
      ),
      cell: ({ getValue }) => propertyCell(prop, getValue()),
      meta: { label: prop.name, ...propertySizing(prop) },
    })
  }

  // updated — always last, right-aligned.
  columns.push({
    id: "updatedAt",
    accessorFn: (e) => e.updatedAt,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="updated" align="right" />
    ),
    cell: ({ row }) => (
      <span
        className="block truncate data text-muted-foreground"
        title={row.original.updatedAt}
      >
        {relativeTime(row.original.updatedAt)}
      </span>
    ),
    meta: {
      label: "updated",
      width: 120,
      headerClassName: "text-right",
      cellClassName: "text-right",
    },
  })

  return columns
}
