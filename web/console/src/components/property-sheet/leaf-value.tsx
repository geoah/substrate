/** One value that holds no structure of its own, read: the shape its datatype
 * earns. A reference is the referent's RecordRef, a state its StateBadge, an
 * enum its authored label on a soft tag, a datetime the day a person would
 * say, a duration its words, and a list of them chips or lines. Mono only for
 * what a reader would copy: URLs, JSON. A declared object or keyed map is the
 * structured view's (structured-value.tsx), which reads its fields through
 * this. */

import type { ReactNode } from "react"

import { friendlyCalendarDay, friendlyDateTime } from "./dates"
import { repeatedLayout } from "./sheet-model"
import { EmptyValue } from "@/components/identity/empty-value"
import { EnumTag } from "@/components/identity/enum-tag"
import { MoneyValue } from "@/components/identity/money-value"
import { QuantityValue } from "@/components/identity/quantity-value"
import { ReferenceValue } from "@/components/identity/reference-value"
import { StateBadge } from "@/components/identity/state-badge"
import { BlobChip } from "@/components/ink/blob-chip"
import { heldBlobInfo, readBlobInfo } from "@/lib/api/blobs"
import { readReference } from "@/lib/api/types"
import { scalarText } from "@/lib/format"
import { coverageWords } from "@/lib/observations-time"
import { splitRecordPath } from "@/lib/record-path"
import { REDACTED, elementSpec, type PropSpec } from "@/lib/record-schema"
import { ExternalLink } from "@/components/external-link"

export function Empty({ children }: { children?: ReactNode }) {
  return <EmptyValue>{children}</EmptyValue>
}

export function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="w-full overflow-x-auto rounded-md border bg-panel px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

function ScalarValue({ spec, value }: { spec: PropSpec; value: unknown }) {
  if (spec.kind === "reference") return <ReferenceValue value={value} />
  if (spec.kind === "money") return <MoneyValue value={value} />
  if (spec.kind === "quantity") return <QuantityValue value={value} />
  const blob =
    spec.kind === "blobref"
      ? (readBlobInfo(value) ?? heldBlobInfo(value))
      : undefined
  if (blob) return <BlobChip blob={blob} />
  if (typeof value === "object" && value !== null) {
    return <JsonBlock value={value} />
  }
  if (spec.kind === "state" && typeof value === "string") {
    return <StateBadge value={value} initial={spec.initial} />
  }
  if (spec.values?.length && typeof value === "string") {
    return <EnumTag prop={spec} value={value} />
  }
  if (spec.kind === "date" && typeof value === "string") {
    return <span title={value}>{friendlyCalendarDay(value)}</span>
  }
  if (spec.kind === "datetime" && typeof value === "string") {
    return <span title={value}>{friendlyDateTime(value)}</span>
  }
  if (spec.kind === "duration" && typeof value === "string") {
    return <span title={value}>{coverageWords(value)}</span>
  }
  if (spec.kind === "url" && typeof value === "string") {
    return (
      <ExternalLink
        href={value}
        className="font-mono text-[12.5px] break-all text-primary-text underline-offset-2 hover:underline"
      >
        {value}
      </ExternalLink>
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

/** A value with no structure of its own, a list of such values included. */
export function LeafValue({ spec, value }: { spec: PropSpec; value: unknown }) {
  if (value === undefined || value === null || value === "") return <Empty />
  if (spec.kind === "secret") {
    return (
      <span className="text-muted-foreground" title={REDACTED}>
        •••••••• <span className="text-faint">sealed</span>
      </span>
    )
  }
  if (!spec.repeated) return <ScalarValue spec={spec} value={value} />
  const item = elementSpec(spec)
  if (!Array.isArray(value)) return <ScalarValue spec={item} value={value} />
  if (!value.length) return <Empty />
  // A shape nobody declared fields for reads one block per item.
  if (item.kind === "json" || item.kind === "object" || item.kind === "map") {
    return (
      <ol className="flex w-full flex-col gap-2">
        {value.map((one: unknown, i) => (
          <li key={i} className="flex min-w-0 items-baseline gap-2">
            {/* A lone item needs no number to tell it apart. */}
            {value.length > 1 && (
              <span className="text-xs text-faint tabular-nums">{i + 1}</span>
            )}
            <div className="min-w-0 flex-1">
              <ScalarValue spec={item} value={one} />
            </div>
          </li>
        ))}
      </ol>
    )
  }
  if (repeatedLayout(item, value) === "chips") {
    // An enum value is its own tag; anything else sits on a plain chip.
    const tagged = Boolean(item.values?.length)
    return (
      <ul
        data-layout="chips"
        className="flex min-w-0 flex-wrap items-center gap-1.5 py-[3px]"
      >
        {value.map((one: unknown, i) => (
          <li
            key={i}
            className={
              tagged
                ? "flex max-w-full"
                : "max-w-full rounded-[5px] bg-hover px-1.5 py-px text-[13px] break-all [&_a]:hover:underline [&>span]:rounded-none [&>span]:bg-transparent [&>span]:p-0"
            }
          >
            <ScalarValue spec={item} value={one} />
          </li>
        ))}
      </ul>
    )
  }
  return (
    <ul data-layout="lines" className="flex w-full min-w-0 flex-col gap-1">
      {value.map((one: unknown, i) => (
        <li key={i} className="min-w-0 break-words">
          <ScalarValue spec={item} value={one} />
        </li>
      ))}
    </ul>
  )
}

/** A value nobody declared, rendered off its own shape. */
export function LooseValue({ value }: { value: unknown }) {
  if (value === undefined || value === null || value === "") return <Empty />
  const held =
    typeof value === "object" && !Array.isArray(value)
      ? readReference(value)
      : undefined
  if (held && splitRecordPath(held.path))
    return <ReferenceValue value={value} />
  if (typeof value === "object") return <JsonBlock value={value} />
  return <span className="break-words">{scalarText(value)}</span>
}
