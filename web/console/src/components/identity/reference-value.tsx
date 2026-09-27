/** One stored reference value, drawn the one way every surface draws it:
 * the referent as a `RecordRef` (never a bare id), and the link data a
 * reference declaration carries beside it (`{ref, <prop>: …}`), because
 * dropping it would hide data the record holds. A value that is not a
 * reference reads as it is. */

import { RecordRef } from "./record-ref"
import { readReference } from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"
import { humanizeName } from "@/lib/record-schema"

function plain(value: unknown): string {
  return typeof value === "object" && value !== null
    ? JSON.stringify(value)
    : String(value)
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
  if (!held || !target) {
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
      <RecordRef kind={target.kind} id={target.id} title={title} />
      {link.map(([k, v]) => (
        <span key={k} className="text-xs text-muted-foreground">
          {humanizeName(k)}: {plain(v)}
        </span>
      ))}
    </span>
  )
}
