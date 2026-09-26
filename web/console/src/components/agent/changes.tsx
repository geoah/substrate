/** The engine-stamped `changes` of a turn, as sentences: one per changelog
 * entry a dispatch (or a decision) wrote — what happened to which record, the
 * record as its mark. Technical mode adds the stored op and the changelog
 * seq, which addresses the exact entry. */

import { RecordRef } from "@/components/identity/record-ref"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { ChangeStamp } from "@/lib/api/transcript"
import { cn } from "@/lib/utils"

const VERBS: Record<string, string> = {
  put: "Saved",
  patch: "Changed",
  delete: "Deleted",
  merge: "Merged",
  split: "Split",
}

function ChangeRow({ change }: { change: ChangeStamp }) {
  const [technical] = useTechnicalDetails()
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-[12.5px]">
      <span
        className={cn(
          "shrink-0 text-muted-foreground",
          change.op === "delete" && "text-destructive"
        )}
      >
        {VERBS[change.op] ?? "Wrote"}
      </span>
      <RecordRef kind={change.kind} id={change.id} className="min-w-0" />
      {technical && (
        <span className="font-mono text-[11.5px] text-faint">
          {change.op || "unknown op"} · seq {change.seq}
        </span>
      )}
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
