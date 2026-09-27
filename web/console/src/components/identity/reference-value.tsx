/** One stored reference value, drawn the one way every surface draws it:
 * the referent as a `RecordRef` (never a bare id), and the link data a
 * reference declaration carries beside it (`{ref, <prop>: …}`), because
 * dropping it would hide data the record holds. A value that is not a
 * reference reads as it is.
 *
 * A reference at core's `kind` kind names a KIND, so it is drawn as the kind's
 * full reference linking to its collection, and a grant's glob
 * (`substrate.reamde.dev/core/kind/*`) as the pattern and what it grants: it
 * names no record, and a `RecordRef` would title it "Untitled kind". */

import { KindRef } from "./kind-ref"
import { RecordRef } from "./record-ref"
import { readReference } from "@/lib/api/types"
import { isRecordId, kindPatternGrant, kindPointer } from "@/lib/kind-pointer"
import { splitRecordPath } from "@/lib/record-path"
import { humanizeName } from "@/lib/record-schema"

function plain(value: unknown): string {
  return typeof value === "object" && value !== null
    ? JSON.stringify(value)
    : String(value)
}

/** A grant's glob: what it grants in words, then the entry as written. */
export function KindPattern({ pattern }: { pattern: string }) {
  return (
    <span
      data-slot="kind-pattern"
      className="inline-flex max-w-full min-w-0 flex-wrap items-baseline gap-x-2"
    >
      <span>{kindPatternGrant(pattern)}</span>
      <code className="font-mono text-[12.5px] [overflow-wrap:anywhere] text-muted-foreground">
        {pattern}
      </code>
    </span>
  )
}

export function ReferenceValue({
  value,
  title,
}: {
  value: unknown
  /** The referent's title, when the surface has already read it; otherwise
   * the mark reads it. */
  title?: string
}) {
  const held = readReference(value)
  const target = held ? splitRecordPath(held.path) : undefined
  const pointer = held ? kindPointer(held.path) : undefined
  if (pointer?.shape === "pattern") {
    return <KindPattern pattern={pointer.pattern} />
  }
  if (!held || !target || !isRecordId(target.id)) {
    return (
      <span className="[overflow-wrap:anywhere]">
        {held ? held.path : plain(value)}
      </span>
    )
  }
  const link = Object.entries(held.properties)
  return (
    <span
      data-slot="reference-value"
      className="inline-flex max-w-full min-w-0 flex-wrap items-center gap-x-2"
    >
      {pointer?.shape === "kind" ? (
        <KindRef kind={pointer.kind} mode="reference" />
      ) : (
        <RecordRef kind={target.kind} id={target.id} title={title} />
      )}
      {link.map(([k, v]) => (
        <span key={k} className="text-xs text-muted-foreground">
          {humanizeName(k)}: {plain(v)}
        </span>
      ))}
    </span>
  )
}
