/** One property's value, read: the shape its datatype earns. A reference is
 * the referent's RecordRef, a state its StateBadge, an enum its authored
 * label on a soft tag, a datetime the day a person would say, a declared
 * object its own fields (nested as deep as the declaration goes), a keyed
 * map its keys beside their values, and a shape nobody declared fields for
 * pretty-printed JSON. Mono only for what a reader would copy: URLs, JSON. */

import type { ReactNode } from "react"

import { friendlyCalendarDay, friendlyDateTime } from "./dates"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { readReference } from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"
import {
  REDACTED,
  elementSpec,
  humanizeName,
  type PropSpec,
} from "@/lib/record-schema"

function isBag(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

export function Empty({ children = "Empty" }: { children?: ReactNode }) {
  return <span className="text-faint">{children}</span>
}

function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="w-full overflow-x-auto rounded-md border bg-panel px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

/** A reference value: the referent, and any link data beside it. */
export function ReferenceMark({ value }: { value: unknown }) {
  const held = readReference(value)
  const target = held ? splitRecordPath(held.path) : undefined
  if (!target) {
    return <span className="break-words">{String(held?.path ?? value)}</span>
  }
  const link = Object.entries(held?.properties ?? {})
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-2">
      <RecordRef kind={target.kind} id={target.id} />
      {link.map(([k, v]) => (
        <span key={k} className="text-xs text-muted-foreground">
          {humanizeName(k)}:{" "}
          {typeof v === "object" ? JSON.stringify(v) : String(v)}
        </span>
      ))}
    </span>
  )
}

/** An object's fields as an indented outline, the name column sized to this
 * block's widest name. */
function Fields({
  rows,
}: {
  rows: Array<{ name: string; label: string; value: unknown; spec?: PropSpec }>
}) {
  return (
    <div className="grid w-full grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-4 gap-y-1 border-l pl-3 text-[13px]">
      {rows.map((row) => (
        <div key={row.name} className="contents">
          <span className="text-faint">{row.label}</span>
          <span className="min-w-0">
            {row.spec ? (
              <DeclaredValue spec={row.spec} value={row.value} />
            ) : (
              <LooseValue value={row.value} />
            )}
          </span>
        </div>
      ))}
    </div>
  )
}

function ObjectBlock({
  spec,
  value,
  dense,
}: {
  spec: PropSpec
  value: unknown
  dense?: boolean
}) {
  if (!isBag(value)) return <JsonBlock value={value} />
  const fields = spec.fields ?? []
  const named = new Set(fields.map((f) => f.name))
  const rows: Array<{
    name: string
    label: string
    value: unknown
    spec?: PropSpec
  }> = []
  for (const field of fields) {
    const held = value[field.name]
    if (dense && (held === undefined || held === null)) continue
    rows.push({
      name: field.name,
      label: field.label,
      value: held,
      spec: field,
    })
  }
  for (const key of Object.keys(value).sort()) {
    if (!named.has(key)) rows.push({ name: key, label: key, value: value[key] })
  }
  if (!rows.length) return <Empty />
  return <Fields rows={rows} />
}

function ScalarValue({
  spec,
  value,
  dense,
}: {
  spec: PropSpec
  value: unknown
  dense?: boolean
}) {
  if (spec.kind === "reference") return <ReferenceMark value={value} />
  if (spec.kind === "object" && spec.fields?.length) {
    return <ObjectBlock spec={spec} value={value} dense={dense} />
  }
  if (typeof value === "object" && value !== null) {
    return <JsonBlock value={value} />
  }
  if (spec.kind === "state" && typeof value === "string") {
    return <StateBadge value={value} initial={spec.initial} />
  }
  if (spec.values?.length && typeof value === "string") {
    const authored = spec.values.find((v) => v.value === value)?.label
    return (
      <span
        title={value}
        className="rounded-[4px] bg-hover px-1.5 py-px text-[13px]"
      >
        {authored || humanizeName(value)}
      </span>
    )
  }
  if (spec.kind === "date" && typeof value === "string") {
    return <span title={value}>{friendlyCalendarDay(value)}</span>
  }
  if (spec.kind === "datetime" && typeof value === "string") {
    return <span title={value}>{friendlyDateTime(value)}</span>
  }
  if (spec.kind === "url" && typeof value === "string") {
    return (
      <a
        href={value}
        target="_blank"
        rel="noreferrer"
        className="font-mono text-[12.5px] break-all text-primary-text underline-offset-2 hover:underline"
      >
        {value}
      </a>
    )
  }
  if (spec.kind === "email" && typeof value === "string") {
    return (
      <a
        href={`mailto:${value}`}
        className="break-all underline-offset-2 hover:underline"
      >
        {value}
      </a>
    )
  }
  if (typeof value === "boolean") return <span>{value ? "Yes" : "No"}</span>
  if (spec.kind === "markdown" || spec.kind === "text") {
    return (
      <span className="line-clamp-3 break-words whitespace-pre-wrap">
        {String(value)}
      </span>
    )
  }
  return <span className="break-words">{String(value)}</span>
}

/** A declared property's value, container and all. */
export function DeclaredValue({
  spec,
  value,
}: {
  spec: PropSpec
  value: unknown
}) {
  if (value === undefined || value === null || value === "") return <Empty />
  if (spec.kind === "secret") {
    return (
      <span className="text-muted-foreground" title={REDACTED}>
        •••••••• <span className="text-faint">sealed</span>
      </span>
    )
  }
  if (spec.keyed) {
    if (!isBag(value)) return <JsonBlock value={value} />
    const entries = Object.entries(value)
    if (!entries.length) return <Empty />
    const item = elementSpec(spec)
    return (
      <Fields
        rows={entries.map(([key, held]) => ({
          name: key,
          label: key,
          value: held,
          spec: item,
        }))}
      />
    )
  }
  if (spec.repeated) {
    if (!Array.isArray(value)) {
      return <ScalarValue spec={elementSpec(spec)} value={value} />
    }
    if (!value.length) return <Empty />
    const item = elementSpec(spec)
    const blocked =
      (item.kind === "object" && Boolean(item.fields?.length)) ||
      item.kind === "json"
    if (blocked) {
      return (
        <ol className="flex w-full flex-col gap-2">
          {value.map((one, i) => (
            <li key={i} className="flex min-w-0 items-baseline gap-2">
              <span className="text-xs text-faint tabular-nums">{i + 1}</span>
              <div className="min-w-0 flex-1">
                <ScalarValue spec={item} value={one} dense />
              </div>
            </li>
          ))}
        </ol>
      )
    }
    return (
      <span className="inline-flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        {value.map((one, i) => (
          <span key={i} className="inline-flex items-center gap-2">
            {i > 0 && item.kind !== "reference" && (
              <span aria-hidden className="text-faint">
                ·
              </span>
            )}
            <ScalarValue spec={item} value={one} />
          </span>
        ))}
      </span>
    )
  }
  return <ScalarValue spec={spec} value={value} />
}

/** A value nobody declared, rendered off its own shape. */
export function LooseValue({ value }: { value: unknown }) {
  if (value === undefined || value === null || value === "") return <Empty />
  const held = isBag(value) ? readReference(value) : undefined
  if (held && splitRecordPath(held.path)) return <ReferenceMark value={value} />
  if (typeof value === "object") return <JsonBlock value={value} />
  return <span className="break-words">{String(value)}</span>
}
