/** One reference VALUE, rendered the one way every surface renders it: the
 * referent's RecordPill, with the declaration's link properties beside it.
 *
 * It lives here rather than in the Properties tab because two surfaces need
 * it and they know a value is a reference by different means. The Properties
 * tab reads the DECLARATION (`type: reference`) and hands whatever the record
 * carries here. A `type: json` value — a change request's `diff` — has no
 * declaration behind it, so it asks `referenceObjects` (in `lib/format`)
 * whether the value carries the served `{ref: "<kind>/<id>"}` shape first. */

import { RecordPill } from "@/components/record-pill"
import { readReference, type KindInfo } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { cellValue } from "@/lib/format"
import { splitRecordPath } from "@/lib/record-path"

/** A stored reference read as the pointer it is: the referent's RecordPill
 * when the registry knows the kind; the raw value, inert, when it does not (a
 * reference may name a kind nobody installed).
 *
 * A reference whose declaration carries LINK DATA stores `{ref, <prop>: …}`
 * rather than the bare path, and the link's properties render beside the pill:
 * dropping them would hide data the record carries. */
export function ReferenceValue({
  value,
  kinds,
}: {
  value: unknown
  kinds: KindInfo[]
}) {
  const held = readReference(value)
  if (!held) {
    return (
      <span className="data break-words">
        {typeof value === "object" ? JSON.stringify(value) : String(value)}
      </span>
    )
  }
  const target = splitRecordPath(held.path)
  const info = target ? kindByIdentity(kinds, target.kind) : undefined
  const pill =
    target && info ? (
      <RecordPill kind={target.kind} id={target.id} />
    ) : (
      <span className="data break-all">{held.path}</span>
    )
  const link = Object.entries(held.properties)
  if (!link.length) return pill
  return (
    <span className="flex max-w-full min-w-0 items-center gap-2">
      {pill}
      <span
        className="truncate data text-xs text-muted-foreground"
        title={JSON.stringify(held.properties)}
      >
        {link.map(([key, held_]) => `${key}: ${cellValue(held_)}`).join(" · ")}
      </span>
    </span>
  )
}
