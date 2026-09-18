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
import { type ReferenceTitles } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

/** The pointer alone: the referent's RecordPill when the registry knows the
 * kind; the raw value, inert, when it does not (a reference may name a kind
 * nobody installed, and a value may not be a reference at all). Neither case
 * drops what the record holds.
 *
 * `titles` is what the pointer is CALLED. A stored reference carries the
 * referent's path and nothing else, so the pill would read as a record id
 * without it; the resolver comes from the surface's own second read (a list's
 * `included`, a record page's batched read) and is absent wherever no such
 * read was made, which is the id fallback `RecordPill` has always had.
 *
 * `dense` is the table-cell voice: a value that cannot be a pill truncates at
 * the column boundary instead of wrapping the row taller, and the pill itself
 * may shrink. */
function Pointer({
  value,
  kinds,
  titles,
  dense,
}: {
  value: unknown
  kinds: KindInfo[]
  titles?: ReferenceTitles
  dense?: boolean
}) {
  const held = readReference(value)
  if (!held) {
    const raw =
      typeof value === "object" ? JSON.stringify(value) : String(value)
    return (
      <span
        className={cn("data", dense ? "truncate" : "break-words")}
        title={dense ? raw : undefined}
      >
        {raw}
      </span>
    )
  }
  const target = splitRecordPath(held.path)
  const info = target ? kindByIdentity(kinds, target.kind) : undefined
  if (!target || !info) {
    return (
      <span
        className={cn("data", dense ? "truncate" : "break-all")}
        title={dense ? held.path : undefined}
      >
        {held.path}
      </span>
    )
  }
  return (
    <RecordPill
      kind={target.kind}
      id={target.id}
      title={titles?.get(held.path)}
      className={dense ? "min-w-0" : undefined}
    />
  )
}

/** A stored reference read as the pointer it is.
 *
 * A reference whose declaration carries LINK DATA stores `{ref, <prop>: …}`
 * rather than the bare path, and the link's properties render beside the pill:
 * dropping them would hide data the record carries. */
export function ReferenceValue({
  value,
  kinds,
  titles,
}: {
  value: unknown
  kinds: KindInfo[]
  /** Record path → the referent's title; absent, the pill reads as the id. */
  titles?: ReferenceTitles
}) {
  const held = readReference(value)
  const pointer = <Pointer value={value} kinds={kinds} titles={titles} />
  if (!held) return pointer
  const link = Object.entries(held.properties)
  if (!link.length) return pointer
  return (
    <span className="flex max-w-full min-w-0 items-center gap-2">
      {pointer}
      <span
        className="truncate data text-xs text-muted-foreground"
        title={JSON.stringify(held.properties)}
      >
        {link.map(([key, held_]) => `${key}: ${cellValue(held_)}`).join(" · ")}
      </span>
    </span>
  )
}

/** The same pointer in a table CELL: one pill per referent, on one line. A
 * repeated reference is a pill each, in stored order.
 *
 * The link PROPERTIES stay off it, unlike the value above: a cell is one line
 * that truncates at its column boundary, and link data reads on the record
 * page, where there is room for it. Without this, every reference column
 * printed `cellValue`'s summary of the served shape — the literal `{ref}`.
 *
 * The click never reaches the row. A table row is itself clickable, so a pill
 * that let the click bubble would navigate to the referent and then have the
 * row navigate somewhere else over it. */
export function ReferenceCell({
  value,
  kinds,
  titles,
}: {
  /** One reference, or the array a repeated one stores. */
  value: unknown
  kinds: KindInfo[]
  /** Record path → the referent's title, off the page's `included` sidecar;
   * absent (the page could not expand), the pill reads as the id. */
  titles?: ReferenceTitles
}) {
  const held = (Array.isArray(value) ? value : [value]).filter(
    (one) => one !== undefined && one !== null && one !== ""
  )
  if (!held.length) return null
  return (
    <span
      className="flex min-w-0 items-center gap-1 overflow-hidden"
      onClick={(e) => e.stopPropagation()}
    >
      {held.map((one, i) => (
        <Pointer key={i} value={one} kinds={kinds} titles={titles} dense />
      ))}
    </span>
  )
}
