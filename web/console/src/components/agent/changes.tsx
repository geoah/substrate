/** The engine-stamped `changes` of a turn, as chips: one per changelog entry
 * a dispatch (or a decision) wrote — the op, and the record it moved, linked
 * where the registry can resolve the kind's plural into a data route. The seq
 * rides as the chip's title: it addresses the delta in the changelog for a
 * reader who wants the exact entry. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { kindsQueryOptions } from "@/lib/api/kinds"
import type { ChangeStamp } from "@/lib/api/transcript"
import { kindByIdentity, splitKind } from "@/lib/definition"

function ChangeChip({ change }: { change: ChangeStamp }) {
  const registry = useQuery(kindsQueryOptions)
  const kind = kindByIdentity(registry.data ?? [], change.kind)
  const { authority, name } = splitKind(change.kind)
  const label = (
    <span className="data">
      {change.op} {name}/{change.id}
    </span>
  )
  const className =
    "inline-flex max-w-full items-center gap-1 truncate rounded-sm border bg-background/60 px-1.5 py-0.5 text-[0.7rem] text-muted-foreground"
  if (!kind) {
    // A kind the registry no longer holds (an uninstalled bundle's) still
    // names what happened; it just has nowhere to link.
    return (
      <span className={className} title={`changelog seq ${change.seq}`}>
        {label}
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$name/$id"
      params={{ authority, name: kind.name, id: change.id }}
      className={`${className} underline-offset-4 hover:underline`}
      title={`changelog seq ${change.seq}`}
    >
      {label}
    </Link>
  )
}

export function ChangesList({ changes }: { changes: ChangeStamp[] }) {
  if (!changes.length) return null
  return (
    <div className="flex flex-wrap gap-1">
      {changes.map((change) => (
        <ChangeChip key={change.seq} change={change} />
      ))}
    </div>
  )
}
