/* eslint-disable react-refresh/only-export-components -- a columns module is
 * a factory of cell renderers, not a page module; nothing here hot-reloads on
 * its own. */

/** The per-kind column factory. ONE grid covers every kind, so the columns
 * come from the declaration: the title first (glyph, title, the Open button),
 * then the states, the stamps the kind's temporal trait binds, its references,
 * its enums and the rest of its short values; paragraphs and blobs never earn
 * a column. The last change closes the row. Headers speak everyday labels
 * (the property key in technical mode) with the declared description one
 * hover away. */

import { Link } from "@tanstack/react-router"
import {
  CalendarIcon,
  ClockIcon,
  HashIcon,
  Maximize2Icon,
  TypeIcon,
} from "lucide-react"

import type { DataTableColumn } from "@/components/data-table/data-table"
import { GridColumnHeader } from "@/components/data-table/data-grid-header"
import { EnumTag } from "@/components/data-table/enum-tag"
import { propertyIcon } from "@/components/data-table/property-icon"
import {
  INDENT_PX,
  TreeToggle,
  useRowTreeNode,
} from "@/components/data-table/data-table-tree"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { readReference } from "@/lib/api/types"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  columnProperties,
  kindByIdentity,
  temporalProperties,
  type DeclaredProperty,
} from "@/lib/definition"
import { cellValue, recordTitle } from "@/lib/format"
import {
  doneStateProperty,
  dueTone,
  friendlyDate,
  friendlyDay,
  isDoneState,
  isDueColumn,
  isEmptyValue,
  propertyLabel,
  subtaskCounts,
  titleProperties,
} from "@/lib/grid-values"
import { untitled } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"
import type { ReferenceTitles } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

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

/** The columns a kind opens WITHOUT, by kind reference: the few core kinds
 * whose declaration is mostly machinery. A kind is listed only where the
 * SHIPPED declaration is known here, because hiding a property of a kind this
 * console did not ship would be guessing at somebody else's vocabulary. */
const DEFAULT_HIDDEN: Record<string, string[]> = {
  "substrate.reamde.dev/core/function": [
    "authority",
    "package",
    "version",
    "source",
    "arguments",
    "returns",
    "permissions",
    "effect",
    "confirmation",
  ],
}

/** The column ids a kind's grid hides until the reader asks for them: the
 * machinery above, and the properties the title is made of, which the title
 * column already shows. A saved preference wins over this entirely. */
export function defaultHiddenColumns(kind: KindInfo): string[] {
  return [
    ...(DEFAULT_HIDDEN[kind.identity] ?? []),
    ...titleProperties(kind),
  ].map(propertyColumnId)
}

// ── widths and icons ────────────────────────────────────────────────────────

const WIDTHS: Record<string, number> = {
  state: 130,
  enum: 140,
  bool: 90,
  datetime: 120,
  date: 120,
  int: 100,
  float: 100,
  decimal: 100,
  email: 240,
  url: 220,
  phone: 170,
  timezone: 160,
  recurrence: 160,
}

function widthOf(prop: DeclaredProperty): number {
  if (prop.kind === "reference") return prop.repeated ? 220 : 190
  return WIDTHS[prop.kind] ?? 180
}

const NUMERIC = new Set(["int", "float", "decimal"])

/** The order columns open in: what a record is (its state), when it is, what
 * it points at, how it is sorted, then everything else. */
function rank(prop: DeclaredProperty): number {
  if (prop.kind === "state") return 0
  if (prop.kind === "reference") return 2
  if (prop.kind === "enum") return 3
  return 4
}

// ── cells ───────────────────────────────────────────────────────────────────

function Empty() {
  return <span className="text-faint">—</span>
}

/** The first of several values, and how many more there are. */
function FirstOf({
  count,
  children,
  title,
}: {
  count: number
  children: React.ReactNode
  title?: string
}) {
  return (
    <span className="flex min-w-0 items-center" title={title}>
      <span className="min-w-0 truncate">{children}</span>
      {count > 1 && (
        <span className="ml-1 shrink-0 text-xs text-faint">+{count - 1}</span>
      )}
    </span>
  )
}

function listOf(value: unknown): unknown[] {
  return (Array.isArray(value) ? value : [value]).filter(
    (v) => !isEmptyValue(v)
  )
}

function ReferenceCell({
  value,
  kinds,
  titles,
}: {
  value: unknown
  kinds: KindInfo[]
  titles?: ReferenceTitles
}) {
  const held = listOf(value)
  const first = readReference(held[0])
  if (!first) return <Empty />
  const target = splitRecordPath(first.path)
  if (!target) {
    return <span className="truncate text-muted-foreground">{first.path}</span>
  }
  return (
    <FirstOf count={held.length}>
      <RecordRef
        kind={target.kind}
        id={target.id}
        title={titles?.get(first.path)}
        // A kind nobody here declares has no page to open.
        link={Boolean(kindByIdentity(kinds, target.kind))}
      />
    </FirstOf>
  )
}

const DUE_CLASSES = {
  done: "text-faint",
  overdue: "text-destructive",
  soon: "text-warning",
}

function DateCell({
  value,
  day,
  time,
  due,
  done,
}: {
  value: string
  /** A bare calendar day (`date`), not an instant. */
  day?: boolean
  time?: boolean
  /** Colour it as a deadline. */
  due?: boolean
  done?: boolean
}) {
  const tone = due ? dueTone(value, undefined, done) : undefined
  return (
    <span
      className={cn("truncate", tone ? DUE_CLASSES[tone] : undefined)}
      title={value}
    >
      {day ? friendlyDay(value) : friendlyDate(value, undefined, { time })}
    </span>
  )
}

function propertyCell(
  prop: DeclaredProperty,
  value: unknown,
  record: SubstrateRecord | undefined,
  ctx: CellContext
): React.ReactNode {
  if (isEmptyValue(value)) return <Empty />
  if (prop.kind === "reference") {
    return <ReferenceCell value={value} kinds={ctx.kinds} titles={ctx.titles} />
  }
  const values = listOf(value)
  const first = values[0]
  if (prop.kind === "state") {
    return <StateBadge value={String(first)} initial={prop.initial} />
  }
  if (prop.kind === "enum") {
    return (
      <FirstOf count={values.length}>
        <EnumTag prop={prop} value={String(first)} />
      </FirstOf>
    )
  }
  if (prop.kind === "bool") {
    return <span className="text-muted-foreground">{first ? "Yes" : "No"}</span>
  }
  if (
    (prop.kind === "datetime" || prop.kind === "date") &&
    typeof first === "string"
  ) {
    const done =
      ctx.doneState && record
        ? isDoneState(
            record.properties[ctx.doneState.name],
            ctx.doneState.initial
          )
        : false
    return (
      <FirstOf count={values.length}>
        <DateCell
          value={first}
          day={prop.kind === "date"}
          due={isDueColumn(prop.name)}
          done={done}
        />
      </FirstOf>
    )
  }
  if (NUMERIC.has(prop.kind)) {
    return <span className="tabular-nums">{cellValue(value)}</span>
  }
  if (prop.keyed) {
    const text = cellValue(value)
    return (
      <span className="block truncate" title={text}>
        {text}
      </span>
    )
  }
  const text = cellValue(first)
  return (
    <FirstOf count={values.length} title={values.map(cellValue).join(", ")}>
      {text}
    </FirstOf>
  )
}

interface CellContext {
  kinds: KindInfo[]
  titles?: ReferenceTitles
  doneState?: DeclaredProperty
}

/** The title cell: the tree's indent and chevron where the grid nests, the
 * kind's glyph, the title (the link to the record), the children's badge, the
 * parent a filtered match belongs to, and the Open button a hovered row shows.
 * The button takes its room from the title rather than covering it, so a long
 * title truncates before it. */
function TitleCell({
  kind,
  record,
  doneState,
  noun,
  titles,
}: {
  kind: KindInfo
  record: SubstrateRecord
  doneState?: DeclaredProperty
  noun: string
  titles?: ReferenceTitles
}) {
  const tree = useRowTreeNode(record.id)
  const title = recordTitle(record.properties)
  const params = {
    authority: kind.authority,
    pkg: kind.package,
    name: kind.name,
    id: record.id,
  }
  const children = tree?.node.childRecords
  const counts = children?.length
    ? subtaskCounts(children, doneState)
    : undefined
  const context = tree?.context ? splitRecordPath(tree.context) : undefined
  return (
    <span
      className="flex min-w-0 items-center gap-1.5"
      style={tree ? { paddingLeft: tree.node.depth * INDENT_PX } : undefined}
    >
      {tree && (
        <TreeToggle
          node={tree.node}
          onToggle={tree.toggle}
          gutter={tree.gutter}
          noun={noun}
        />
      )}
      <KindGlyph kind={kind} size="xs" />
      <Link
        to="/data/$authority/$pkg/$name/$id"
        params={params}
        title={title || undefined}
        className={cn(
          "min-w-0 truncate underline-offset-[3px] outline-none hover:underline hover:decoration-border-strong focus-visible:underline",
          !title && "text-muted-foreground"
        )}
      >
        {title || untitled(kind)}
      </Link>
      {counts && (
        <span
          className="shrink-0 rounded-full border border-border-strong px-1.5 text-[11.5px] leading-[18px] font-normal whitespace-nowrap text-faint"
          title={
            counts.done !== undefined
              ? `${counts.done} of ${counts.total} done`
              : `${counts.total} under this`
          }
        >
          {counts.done !== undefined
            ? `${counts.done}/${counts.total}`
            : counts.total}
        </span>
      )}
      {context && tree?.context && (
        <span
          data-slot="tree-context"
          className="flex max-w-[40%] min-w-0 shrink-0 items-center gap-1 text-[12px] font-normal whitespace-nowrap text-faint"
        >
          in
          <RecordRef
            kind={context.kind}
            id={context.id}
            title={titles?.get(tree.context)}
            className="min-w-0 text-muted-foreground [&_[data-slot=kind-glyph]]:hidden"
          />
        </span>
      )}
      <Link
        to="/data/$authority/$pkg/$name/$id"
        params={params}
        aria-label={`Open ${title || untitled(kind)}`}
        tabIndex={-1}
        className="ml-auto hidden h-[22px] shrink-0 items-center gap-1 rounded-[5px] border border-border-strong bg-background px-[7px] text-[11.5px] font-medium text-muted-foreground no-underline shadow-[0_1px_2px_rgba(0,0,0,.06)] group-hover/row:inline-flex hover:text-foreground"
      >
        <Maximize2Icon aria-hidden className="size-3" />
        Open
      </Link>
    </span>
  )
}

export interface BuildColumnsOptions {
  /** Headers show property keys and the reference mode shows everything. */
  technical?: boolean
  /** What a nested row's children are called ("subtasks"). */
  childNoun?: string
}

export function buildColumns(
  kind: KindInfo,
  /** The registry, so a reference cell can tell a kind it can route to from
   * one nobody installed. */
  kinds: KindInfo[],
  /** Record path → the referent's title, off the page's `included` sidecar
   * (`expand=`). Absent, each reference reads its own title (batched). */
  titles?: ReferenceTitles,
  opts: BuildColumnsOptions = {}
): DataTableColumn<SubstrateRecord>[] {
  const technical = opts.technical ?? false
  const declared = columnProperties(kind)
  const doneState = doneStateProperty(declared)
  const ctx: CellContext = { kinds, titles, doneState }
  const labelOf = (name: string) => (technical ? name : propertyLabel(name))
  const columns: DataTableColumn<SubstrateRecord>[] = []

  columns.push({
    id: "title",
    accessorFn: (e) => recordTitle(e.properties),
    enableHiding: false,
    header: ({ column }) => (
      <GridColumnHeader
        column={column}
        label={technical ? "title" : "Name"}
        icon={TypeIcon}
        mono={technical}
      />
    ),
    cell: ({ row }) => (
      <TitleCell
        kind={kind}
        record={row.original}
        doneState={doneState}
        noun={opts.childNoun ?? "rows"}
        titles={titles}
      />
    ),
    meta: { label: technical ? "title" : "Name", width: 300 },
  })

  if (technical) {
    columns.push({
      id: "id",
      accessorFn: (e) => e.id,
      enableSorting: false,
      header: ({ column }) => (
        <GridColumnHeader column={column} label="id" icon={HashIcon} mono />
      ),
      cell: ({ row }) => (
        <span
          className="block truncate font-mono text-[12px] text-muted-foreground"
          title={row.original.id}
        >
          {row.original.id}
        </span>
      ),
      meta: { label: "id", width: 150 },
    })
  }

  const ordered = [...declared].sort((a, b) => rank(a) - rank(b))
  const states = ordered.filter((p) => rank(p) === 0)
  const rest = ordered.filter((p) => rank(p) > 0)

  const propertyColumn = (
    prop: DeclaredProperty
  ): DataTableColumn<SubstrateRecord> => {
    const numeric = NUMERIC.has(prop.kind)
    return {
      id: propertyColumnId(prop.name),
      accessorFn: (e) => e.properties[prop.name],
      enableSorting: !prop.repeated && !RESERVED_SORT_NAMES.has(prop.name),
      header: ({ column }) => (
        <GridColumnHeader
          column={column}
          label={labelOf(prop.name)}
          icon={propertyIcon(prop)}
          description={prop.description}
          mono={technical}
          align={numeric ? "right" : "left"}
        />
      ),
      cell: ({ getValue, row }) =>
        propertyCell(prop, getValue(), row?.original, ctx),
      meta: {
        label: labelOf(prop.name),
        width: widthOf(prop),
        ...(numeric ? { cellClassName: "text-right" } : {}),
      },
    }
  }

  columns.push(...states.map(propertyColumn))

  // The stamps the kind's temporal trait binds are system columns: sortable
  // by their own name, described by the declaration when it declares them.
  const all = new Map(
    // columnProperties drops the temporal names; the declaration still
    // describes them.
    Object.entries(
      (kind.definition?.properties ?? {}) as Record<
        string,
        { description?: unknown }
      >
    )
  )
  for (const name of temporalProperties(kind)) {
    const description = all.get(name)?.description
    const withTime = !isDueColumn(name)
    columns.push({
      id: name,
      accessorFn: (e) => e.properties[name],
      header: ({ column }) => (
        <GridColumnHeader
          column={column}
          label={labelOf(name)}
          icon={CalendarIcon}
          description={
            typeof description === "string" ? description : undefined
          }
          mono={technical}
        />
      ),
      cell: ({ getValue, row }) => {
        const value = getValue()
        if (typeof value !== "string" || !value) return <Empty />
        const record = row?.original
        const done =
          doneState && record
            ? isDoneState(record.properties[doneState.name], doneState.initial)
            : false
        return (
          <DateCell
            value={value}
            time={withTime}
            due={isDueColumn(name)}
            done={done}
          />
        )
      },
      meta: { label: labelOf(name), width: withTime ? 150 : 120 },
    })
  }

  columns.push(...rest.map(propertyColumn))

  columns.push({
    id: "updatedAt",
    accessorFn: (e) => e.updatedAt,
    header: ({ column }) => (
      <GridColumnHeader
        column={column}
        label={technical ? "updatedAt" : "Updated"}
        icon={ClockIcon}
        mono={technical}
      />
    ),
    cell: ({ row }) => (
      <span
        className="truncate text-muted-foreground"
        title={row.original.updatedAt}
      >
        {friendlyDate(row.original.updatedAt)}
      </span>
    ),
    meta: { label: technical ? "updatedAt" : "Updated", width: 120 },
  })

  return columns
}

/** How a column's value reads for the empty-column test: a declared
 * property's value, or a system column's. */
export function columnValue(
  record: SubstrateRecord,
  columnId: string
): unknown {
  if (columnId === "title") return recordTitle(record.properties)
  if (columnId === "updatedAt") return record.updatedAt
  if (columnId === "id") return record.id
  return record.properties[sortPropertyOf(columnId)]
}
