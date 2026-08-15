/** What is waiting to change THIS record (#54.3).
 *
 * The pointer only ever existed from the request's side: the queue names its
 * target, the target named nothing back. So a reader on a record page had no
 * way to know three proposals were queued against it — they would edit it by
 * hand and find out at the conflict.
 *
 * This is the reverse read, and it sits above the tabs rather than inside one:
 * "somebody is proposing to change this" is context for everything on the
 * page, not a fifth thing to go looking for. It renders nothing at all when
 * nothing is pending, so a quiet record stays quiet.
 *
 * Proposed only. A decided request is history — the Activity tab's business —
 * and listing it here would turn a call to action into a log. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { FilePenLineIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { changeRequestsForTargetQueryOptions } from "@/lib/api/changerequests"
import { changeOp } from "@/lib/changerequests"
import { recordTitle, relativeTime } from "@/lib/format"

/** Enough to say "these, and how many more". The queue page is one click away
 * and is where a reader goes to work through them. */
const TOP_N = 3

export function TargetingRequests({ kind, id }: { kind: string; id: string }) {
  // Passed straight through, NOT spread into an object literal: the spread
  // widens `data` to `any` and takes every field access off the type checker's
  // hands, which is exactly how a field that does not exist gets read.
  const requests = useQuery(
    changeRequestsForTargetQueryOptions({
      kind,
      id,
      decision: "proposed",
      // One more than shown, so "and N more" is answerable without a second
      // round trip or a bounded count walk.
      first: TOP_N + 1,
    })
  )

  const rows = requests.data?.records ?? []
  // Silence is the common case and the right one: a record with nothing
  // pending should not carry an empty box saying so. An error is silent too —
  // this is a sidecar, and failing it loudly would put a red zone on a page
  // whose actual content loaded.
  if (!rows.length) return null

  const shown = rows.slice(0, TOP_N)
  const more = rows.length - shown.length

  return (
    <div className="mx-6 mb-3 flex flex-col gap-1.5 rounded-md border border-amber-500/40 bg-amber-500/5 px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-xs font-medium">
        <FilePenLineIcon className="size-3.5" />
        {rows.length === 1
          ? "A change is proposed for this record"
          : `${shown.length}${more > 0 ? "+" : ""} changes are proposed for this record`}
      </div>
      <ul className="flex flex-col gap-1">
        {shown.map((r) => (
          <li key={r.id} className="flex items-center gap-2 text-xs">
            <Badge variant="secondary" className="shrink-0">
              {changeOp(r) ?? "change"}
            </Badge>
            <Link
              to="/change-requests/$id"
              params={{ id: r.id }}
              className="truncate underline-offset-2 hover:underline"
            >
              {recordTitle(r.properties) || r.id}
            </Link>
            <span
              className="ml-auto shrink-0 data text-muted-foreground"
              title={r.updatedAt}
            >
              {relativeTime(r.updatedAt)}
            </span>
          </li>
        ))}
      </ul>
      {more > 0 && (
        <span className="text-xs text-muted-foreground">
          and more waiting in the queue
        </span>
      )}
    </div>
  )
}
