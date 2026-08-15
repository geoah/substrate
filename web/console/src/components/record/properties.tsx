/** The Properties tab (issue #38): the record's data field by field, instead
 * of making a reader parse the manifest's YAML. Each declared property renders
 * through the shape its datatype earns: prose (`text`, `markdown`) as a
 * paragraph block, `json`/`object`/keyed maps as pretty-printed JSON, a
 * `reference` as a link to its referent, a `state` as its badge, an enum as
 * its authored label, a `datetime` as the console's stamp with the wire value
 * on hover, a `secret` as the redaction sentinel, and everything else as one
 * compact line. Declared-but-unset properties still show, saying "not set",
 * so the kind's whole shape is readable off one record; values the kind never
 * declared show too, marked as such, because hiding data a record carries
 * would make this view lie. Edges follow beneath, each target a peek-able
 * link. Read-only: Edit is the page's affordance, not this tab's. */

import { Link } from "@tanstack/react-router"
import { ListIcon } from "lucide-react"

import { RecordPeek } from "@/components/record-peek"
import { StateBadge } from "@/components/state-badge"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { cellValue, shortDateTime } from "@/lib/format"
import { splitRecordPath } from "@/lib/record-path"
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

/** One line of the view: a declared property (spec present, value maybe not)
 * or a value the record carries without a declaration behind it. */
interface PropertyRow {
  name: string
  value: unknown
  spec?: PropSpec
  /** The kind is known and does not declare this name. */
  undeclared?: boolean
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

/** A stored reference read as the link it is: the referent's id, routed to its
 * detail page when the registry knows the kind; the raw value, inert, when it
 * does not (a reference may name a kind nobody installed). */
function ReferenceValue({
  value,
  kinds,
}: {
  value: unknown
  kinds: KindInfo[]
}) {
  if (typeof value !== "string" || !value) {
    return <span className="data break-words">{String(value)}</span>
  }
  const target = splitRecordPath(value)
  const info = target ? kindByIdentity(kinds, target.kind) : undefined
  if (!target || !info) {
    return <span className="data break-all">{value}</span>
  }
  return (
    <Link
      to="/data/$authority/$plural/$id"
      params={{ authority: info.authority, plural: info.plural, id: target.id }}
      className="data break-all underline-offset-4 hover:underline"
      title={value}
    >
      {target.id}
    </Link>
  )
}

/** One scalar, by its declared datatype. Containers are the caller's job. */
function ScalarValue({
  spec,
  value,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  kinds: KindInfo[]
}) {
  if (spec.kind === "reference") {
    return <ReferenceValue value={value} kinds={kinds} />
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
  if (spec.keyed || isObjectKind(spec.kind)) {
    return <JsonBlock value={value} />
  }
  if (spec.repeated) {
    if (!Array.isArray(value)) {
      return (
        <ScalarValue spec={elementSpec(spec)} value={value} kinds={kinds} />
      )
    }
    if (!value.length) return <NotSet>none</NotSet>
    const item = elementSpec(spec)
    return (
      <ul className="flex flex-col gap-0.5">
        {value.map((one, i) => (
          <li key={i}>
            <ScalarValue spec={item} value={one} kinds={kinds} />
          </li>
        ))}
      </ul>
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
  const doc = row.spec
    ? [typeLabel(row.spec), row.spec.description].filter(Boolean).join(" — ")
    : undefined
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-baseline gap-2 text-xs">
        <span className={doc ? "cursor-help data" : "data"} title={doc}>
          {row.name}
        </span>
        {row.spec && (
          <span className="truncate text-muted-foreground">
            {typeLabel(row.spec)}
          </span>
        )}
        {row.undeclared && (
          <span className="text-muted-foreground/70">undeclared</span>
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
  /** The registry, so a reference and an edge target resolve to routes. */
  kinds: KindInfo[]
}) {
  const rows = rowsOf(record, kind)
  const edges = Object.entries(record.edges ?? {})
    .filter(([, targets]) => (targets ?? []).length > 0)
    .sort(([a], [b]) => a.localeCompare(b))

  if (!rows.length && !edges.length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ListIcon />
          </EmptyMedia>
          <EmptyTitle>No data</EmptyTitle>
          <EmptyDescription>
            This record carries no properties and no edges.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4 px-6 py-4">
      {rows.map((row) => (
        <Row key={row.name} row={row} kinds={kinds} />
      ))}
      {edges.length > 0 && (
        <div className="flex flex-col gap-3 border-t pt-4">
          <h2 className="text-xs font-medium text-muted-foreground">Edges</h2>
          {edges.map(([rel, targets]) => (
            <div key={rel} className="flex min-w-0 flex-col gap-1">
              <span className="data text-xs">{rel}</span>
              <div className="flex min-w-0 flex-col gap-0.5 text-sm">
                {(targets ?? []).map((target) => (
                  <span
                    key={`${target.kind} ${target.id}`}
                    className="flex min-w-0 items-baseline gap-2"
                  >
                    <RecordPeek target={target} types={kinds} />
                    {/* The EDGE's own properties ride the target on the wire;
                        dropping them would hide data the manifest shows. */}
                    {target.properties &&
                      Object.keys(target.properties).length > 0 && (
                        <span
                          className="truncate data text-xs text-muted-foreground"
                          title={JSON.stringify(target.properties)}
                        >
                          {Object.entries(target.properties)
                            .map(([k, v]) => `${k}: ${cellValue(v)}`)
                            .join(" · ")}
                        </span>
                      )}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
