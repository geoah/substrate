/** The Properties tab (issue #38): the record's data field by field, instead
 * of making a reader parse the manifest's YAML. Each declared property renders
 * through the shape its datatype earns: prose (`text`, `markdown`) as a
 * paragraph block, a DECLARED `object` as its own fields — one labelled row
 * per field, nested as deep as the declaration goes, a repeated one as a
 * numbered stack of those blocks and a `keyed:` map as its author-chosen keys
 * beside their values — a shape nobody declared fields for (`json`, a bare
 * `object`) as pretty-printed JSON, a `reference` as its referent's RecordPill,
 * a `state` as its badge, an enum as its authored label, a `datetime` as the
 * console's stamp with the wire value on hover, a `secret` as the redaction
 * sentinel, and everything else as one compact line. Declared-but-unset
 * properties still show, saying "not set", so the kind's whole shape is
 * readable off one record; values the kind never declared show too, marked as
 * such, because hiding data a record carries would make this view lie. A
 * reference carrying LINK DATA renders the referent's pill with the link's own
 * properties beside it. Under the properties sits **Linked from**
 * (`linked-from.tsx`): the records whose mapping-owned subject slot points at
 * this one, which nothing on this record's own properties would show. Read-only:
 * Edit is the page's affordance, not this tab's. */

import * as React from "react"

import { ListIcon } from "lucide-react"

import { LinkedFromSection } from "@/components/record/linked-from"
import { ReferenceValue } from "@/components/record/reference-value"
import { StateBadge } from "@/components/state-badge"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import { shortDateTime } from "@/lib/format"
import {
  REDACTED,
  elementSpec,
  humanizeName,
  isObjectKind,
  propSpecsByName,
  systemSpecs,
  typeLabel,
  type PropSpec,
} from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** One line of the view: a declared property (spec present, value maybe not)
 * or a value the record carries without a declaration behind it. */
interface PropertyRow {
  name: string
  value: unknown
  spec?: PropSpec
  /** The kind is known and does not declare this name. */
  undeclared?: boolean
}

/** What a name says about itself on hover: its datatype and the declaration's
 * one-liner. The nested blocks have no room to print either, so they say both
 * here rather than dropping them. */
function docOf(spec?: PropSpec): string | undefined {
  if (!spec) return undefined
  return [typeLabel(spec), spec.description].filter(Boolean).join(" · ")
}

/** A plain bag of keys: what an `object` declaration expects to find, and what
 * a field-by-field rendering needs. A declared object holding anything else is
 * a record disagreeing with its kind, and reads as the JSON it is. */
function isBag(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function NotSet({ children = "not set" }: { children?: React.ReactNode }) {
  return <span className="text-muted-foreground/70">{children}</span>
}

/** A stored empty string. Absence says "not set"; this is a value somebody
 * wrote, and hiding the difference would make this view disagree with the
 * manifest. */
function EmptyString() {
  return (
    <span className="data text-muted-foreground/70" title="an empty string">
      ""
    </span>
  )
}

/** A paragraph-sized value: prose datatypes, and any string too long or too
 * broken to be a line. Whitespace is the author's. */
function ProseBlock({ text }: { text: string }) {
  return (
    <div className="rounded-lg border bg-muted/30 px-3 py-2 text-sm break-words whitespace-pre-wrap">
      {text}
    </div>
  )
}

/** JSON-shaped values, pretty-printed. The one honest rendering of a shape
 * nobody declared fields for. */
function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="overflow-x-auto rounded-lg border bg-muted/30 px-3 py-2 data text-xs break-words whitespace-pre-wrap">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

/** Whether a value wants the WHOLE width under its name rather than a cell
 * beside it: another object, a keyed map, a JSON shape, a paragraph. Anything
 * that fits on a line stays on the line. */
function wantsItsOwnLine(spec: PropSpec | undefined, value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  if (!spec) return typeof value === "object"
  // A pointer is a pill, however it is stored: the served `{ref}` shape is an
  // object and reads as one line, so it is not one of these.
  if (spec.kind === "reference") return false
  if (spec.keyed) return true
  if (isObjectKind(spec.kind)) return true
  if (spec.kind === "text" || spec.kind === "markdown") return true
  // A value disagreeing with its kind: a bag renders as the JSON block it is.
  return isBag(value)
}

/** The fields of one object, as an indented OUTLINE rather than a table.
 *
 * A nested table costs a fixed label column per level, so a grant three
 * objects deep spends most of a 3xl page on gutters and leaves its values in a
 * column too narrow to read; the dialect allows four. This spends a hairline
 * and 12px per level instead, and sizes the name column to the widest name IN
 * THIS BLOCK, so the level's own fields align without charging the level below
 * for it. A field whose value is itself a block takes the full width under its
 * name, which is the same shape the page gives a record's own properties. */
function NestedRows({
  rows,
  guide = true,
  kinds,
}: {
  rows: PropertyRow[]
  /** An item of a container is marked by its ordinal already; a second rule
   * beside the number says nothing the number did not. */
  guide?: boolean
  kinds: KindInfo[]
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-4 gap-y-1.5",
        guide && "border-l pl-3"
      )}
    >
      {rows.map((row) => {
        const name = (
          <span
            className={cn(
              "data text-xs text-muted-foreground",
              row.spec?.description && "cursor-help"
            )}
            title={docOf(row.spec)}
          >
            {row.name}
            {row.undeclared && (
              <span className="text-muted-foreground/70">
                {" \u00b7 undeclared"}
              </span>
            )}
          </span>
        )
        const value = (
          <div className="min-w-0 text-sm">
            {row.spec ? (
              <DeclaredValue spec={row.spec} value={row.value} kinds={kinds} />
            ) : (
              <LooseValue value={row.value} />
            )}
          </div>
        )
        if (wantsItsOwnLine(row.spec, row.value)) {
          return (
            <div
              key={row.name}
              className="col-span-2 flex min-w-0 flex-col gap-1"
            >
              {name}
              {value}
            </div>
          )
        }
        return (
          <React.Fragment key={row.name}>
            {name}
            {value}
          </React.Fragment>
        )
      })}
    </div>
  )
}

/** A DECLARED object, read as its fields rather than as a blob. Two rules
 * decide what a block lists. A single object says the whole declared shape,
 * unset fields included, exactly as the page does for the record itself: the
 * knob that exists and holds nothing is worth knowing about. An item of a
 * CONTAINER says only what it carries, because the shape is said once by the
 * declaration and repeating "not set" once per row says nothing new. Either
 * way a key the declaration never named is shown and marked, since hiding data
 * the record holds would make this view lie. */
function ObjectBlock({
  spec,
  value,
  dense,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  /** An item of a container: list what is here, not the whole shape, and wear
   * the ordinal rather than a second guide rule. */
  dense?: boolean
  kinds: KindInfo[]
}) {
  if (!isBag(value)) return <JsonBlock value={value} />
  const fields = spec.fields ?? []
  const named = new Set(fields.map((field) => field.name))
  const rows: PropertyRow[] = []
  for (const field of fields) {
    const held = value[field.name]
    if (dense && (held === undefined || held === null)) continue
    rows.push({ name: field.name, value: held, spec: field })
  }
  for (const key of Object.keys(value).sort()) {
    if (named.has(key)) continue
    rows.push({ name: key, value: value[key], undeclared: true })
  }
  if (!rows.length) return <NotSet>empty</NotSet>
  return <NestedRows rows={rows} guide={!dense} kinds={kinds} />
}

/** A KEYED map: the author names the keys, so each key heads the value it maps
 * to, and the value renders off the map's own datatype. */
function KeyedBlock({
  spec,
  value,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  kinds: KindInfo[]
}) {
  if (!isBag(value)) return <JsonBlock value={value} />
  const entries = Object.entries(value)
  if (!entries.length) return <NotSet>empty</NotSet>
  const item = elementSpec(spec)
  return (
    <NestedRows
      rows={entries.map(([key, held]) => ({
        name: key,
        value: held,
        spec: item,
      }))}
      kinds={kinds}
    />
  )
}

/** One scalar, by its declared datatype. Containers are the caller's job. */
function ScalarValue({
  spec,
  value,
  dense,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  dense?: boolean
  kinds: KindInfo[]
}) {
  // Before the object arm: a reference carrying link data IS an object, and
  // rendering it as fields would bury the pointer in a block.
  if (spec.kind === "reference") {
    return <ReferenceValue value={value} kinds={kinds} />
  }
  // A declaration that names the fields is the one thing that beats JSON here:
  // `json` and a bare `object` still read as the shape nobody owns.
  if (spec.kind === "object" && spec.fields?.length) {
    return <ObjectBlock spec={spec} value={value} dense={dense} kinds={kinds} />
  }
  if (typeof value === "object" && value !== null) {
    return <JsonBlock value={value} />
  }
  if (spec.kind === "state" && typeof value === "string") {
    return <StateBadge value={value} initial={spec.initial} />
  }
  if (spec.kind === "datetime" && typeof value === "string") {
    return (
      <span className="data" title={value}>
        {shortDateTime(value)}
      </span>
    )
  }
  if (spec.kind === "url" && typeof value === "string") {
    return (
      <a
        href={value}
        target="_blank"
        rel="noreferrer"
        className="data break-all underline-offset-4 hover:underline"
      >
        {value}
      </a>
    )
  }
  if (spec.values?.length && typeof value === "string") {
    const authored = spec.values.find((v) => v.value === value)?.label
    return <span title={value}>{authored || humanizeName(value)}</span>
  }
  if (spec.kind === "text" || spec.kind === "markdown") {
    return <ProseBlock text={String(value)} />
  }
  return <span className="data break-words">{String(value)}</span>
}

/** A declared property's value, container and all. */
function DeclaredValue({
  spec,
  value,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  kinds: KindInfo[]
}) {
  if (value === undefined || value === null) return <NotSet />
  if (value === "") return <EmptyString />
  // The read serves a sealed value as its sentinel already; saying it in the
  // muted voice keeps a copied page from looking like it leaked something.
  if (spec.kind === "secret") {
    return <span className="data text-muted-foreground">{REDACTED}</span>
  }
  // The CONTAINER decides before the datatype does, exactly as the form's
  // control does: a keyed map of objects is keys, not one object.
  if (spec.keyed) {
    return <KeyedBlock spec={spec} value={value} kinds={kinds} />
  }
  if (spec.repeated) {
    if (!Array.isArray(value)) {
      return (
        <ScalarValue spec={elementSpec(spec)} value={value} kinds={kinds} />
      )
    }
    if (!value.length) return <NotSet>none</NotSet>
    const item = elementSpec(spec)
    // The ordinal rides the gutter of a repeated OBJECT: a stack of blocks
    // that all look alike is unreadable without a way to say which is which,
    // and a declaration may hold the order (an agent's tools do).
    const blocked = item.kind === "object" && Boolean(item.fields?.length)
    return (
      <ol className={cn("flex flex-col", blocked ? "gap-2" : "gap-0.5")}>
        {value.map((one, i) => (
          <li
            key={i}
            className={
              blocked ? "flex min-w-0 items-baseline gap-2" : undefined
            }
          >
            {blocked && (
              <span className="data text-xs text-muted-foreground/70 tabular-nums">
                {i + 1}
              </span>
            )}
            <div className={blocked ? "min-w-0 flex-1" : undefined}>
              <ScalarValue spec={item} value={one} dense kinds={kinds} />
            </div>
          </li>
        ))}
      </ol>
    )
  }
  return <ScalarValue spec={spec} value={value} kinds={kinds} />
}

/** A value nobody declared: rendered off its own shape, since there is no
 * datatype to ask. */
function LooseValue({ value }: { value: unknown }) {
  if (value === undefined || value === null) return <NotSet />
  if (value === "") return <EmptyString />
  if (typeof value === "object") return <JsonBlock value={value} />
  const text = String(value)
  if (typeof value === "string" && (text.includes("\n") || text.length > 120)) {
    return <ProseBlock text={text} />
  }
  return <span className="data break-words">{text}</span>
}

function Row({ row, kinds }: { row: PropertyRow; kinds: KindInfo[] }) {
  const doc = docOf(row.spec)
  return (
    <div className="flex min-w-0 flex-col gap-1">
      {/* The name heads its value: same size, heavier weight — a header that
          renders smaller than its body reads as a footnote. */}
      <div className="flex items-baseline gap-2">
        <span
          className={
            doc
              ? "cursor-help data text-sm font-medium"
              : "data text-sm font-medium"
          }
          title={doc}
        >
          {row.name}
        </span>
        {row.spec && (
          <span className="truncate text-xs text-muted-foreground">
            {typeLabel(row.spec)}
          </span>
        )}
        {row.undeclared && (
          <span className="text-xs text-muted-foreground/70">undeclared</span>
        )}
      </div>
      <div className="min-w-0 text-sm">
        {row.spec ? (
          <DeclaredValue spec={row.spec} value={row.value} kinds={kinds} />
        ) : (
          <LooseValue value={row.value} />
        )}
      </div>
    </div>
  )
}

/** The rows, in reading order: the system slots the record actually carries
 * (title, body, a temporal trait's stamps), then every declared property,
 * set or not, in name order (the read surfaces' order), then whatever the
 * record holds that the declaration never named. */
function rowsOf(record: SubstrateRecord, kind?: KindInfo): PropertyRow[] {
  const declared = kind ? propSpecsByName(kind) : []
  const rows: PropertyRow[] = []
  const named = new Set(declared.map((s) => s.name))
  if (kind) {
    for (const spec of systemSpecs(kind)) {
      if (named.has(spec.name)) continue
      named.add(spec.name)
      const value = record.properties[spec.name]
      // A system slot is legal on every record and absent on most; an empty
      // row per absent slot would say nothing.
      if (value === undefined || value === null) continue
      rows.push({ name: spec.name, value, spec })
    }
  }
  for (const spec of declared) {
    rows.push({ name: spec.name, value: record.properties[spec.name], spec })
  }
  for (const name of Object.keys(record.properties).sort()) {
    if (named.has(name)) continue
    rows.push({
      name,
      value: record.properties[name],
      undeclared: Boolean(kind),
    })
  }
  return rows
}

export function PropertiesRail({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  /** The record's own declaration; undefined when the registry lacks it, in
   * which case every value renders off its shape alone. */
  kind?: KindInfo
  /** The registry, so a reference resolves to a route. */
  kinds: KindInfo[]
}) {
  const rows = rowsOf(record, kind)
  // The inbound mapping-owned links ride on the single-record read, so they
  // are here without a second request; a kind no mapping targets carries the
  // key not at all (decision 0088).
  const links = record.linkedFrom ?? []

  if (!rows.length && !links.length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ListIcon />
          </EmptyMedia>
          <EmptyTitle>No data</EmptyTitle>
          <EmptyDescription>This record has no properties.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4 px-6 py-4">
      {rows.map((row) => (
        <Row key={row.name} row={row} kinds={kinds} />
      ))}
      <LinkedFromSection links={links} kinds={kinds} />
    </div>
  )
}
