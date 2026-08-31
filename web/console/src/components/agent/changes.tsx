/** The engine-stamped `changes` of a turn, as rows: one per changelog entry a
 * dispatch (or a decision) wrote — the op as a badge in the change-request
 * voice (`delete` wears destructive, a reviewer must never have to read the
 * label), and the record it moved as the pill every reference renders as. The
 * seq rides as the row's title: it addresses the delta in the changelog for a
 * reader who wants the exact entry. */

import { RecordPill } from "@/components/record-pill"
import { Badge } from "@/components/ui/badge"
import type { ChangeStamp } from "@/lib/api/transcript"

function ChangeRow({ change }: { change: ChangeStamp }) {
  return (
    <div
      className="flex min-w-0 items-center gap-1.5"
      title={`changelog seq ${change.seq}`}
    >
      <Badge
        variant={
          change.op === "delete" || !change.op ? "destructive" : "outline"
        }
        className="shrink-0 data font-normal"
      >
        {change.op || "unknown op"}
      </Badge>
      <RecordPill kind={change.kind} id={change.id} className="min-w-0" />
      <span className="ml-auto shrink-0 data text-[0.65rem] text-muted-foreground">
        seq {change.seq}
      </span>
    </div>
  )
}

export function ChangesList({ changes }: { changes: ChangeStamp[] }) {
  if (!changes.length) return null
  return (
    <div className="flex flex-col gap-1">
      {changes.map((change) => (
        <ChangeRow key={change.seq} change={change} />
      ))}
    </div>
  )
}
