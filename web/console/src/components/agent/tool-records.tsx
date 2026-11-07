/** The records one tool call worked on, under its line and without opening
 * it: the first few as chips that open the record, and the rest on asking.
 * A call that worked on one record names it in its line instead and keeps
 * the link in its details, so the line is not said twice. */

import { useState } from "react"

import { RecordRef } from "@/components/identity/record-ref"
import { CHANGE_VERBS, type LandedChange } from "@/lib/agent-chat"

/** How many chips show before "and N more". */
export const FIRST_RECORDS = 3

export function RecordChips({
  records,
  total = records.length,
  lead,
  label,
}: {
  records: { kind: string; id: string; title?: string }[]
  /** How many there were, where more came back than the list holds. */
  total?: number
  /** Words before the chips, for a call whose line says something else. */
  lead?: string
  /** What the list is, for a screen reader. */
  label: string
}) {
  const [all, setAll] = useState(false)
  const shown = all ? records : records.slice(0, FIRST_RECORDS)
  const more = total - shown.length
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 pl-1 text-[12.5px] text-muted-foreground">
      {lead && <span className="shrink-0">{lead}</span>}
      <ul aria-label={label} className="contents">
        {shown.map((r, i) => (
          <li key={`${r.kind}/${r.id}/${i}`} className="max-w-full min-w-0">
            <RecordRef
              kind={r.kind}
              id={r.id}
              title={r.title}
              variant="chip"
              className="max-w-[22rem]"
            />
          </li>
        ))}
      </ul>
      {more > 0 &&
        (records.length > shown.length ? (
          <button
            type="button"
            onClick={() => setAll(true)}
            className="cursor-pointer text-faint underline-offset-2 hover:text-muted-foreground hover:underline"
          >
            and {more} more
          </button>
        ) : (
          <span className="text-faint">and {more} more</span>
        ))}
    </div>
  )
}

/** A call's changes as chips, one run of chips per verb in the order they
 * happened: "Saved [a] [b]", then "Deleted [c]". */
export function ChangeChips({ changes }: { changes: LandedChange[] }) {
  const runs: { op: string; changes: LandedChange[] }[] = []
  for (const change of changes) {
    const last = runs[runs.length - 1]
    if (last && last.op === change.op) last.changes.push(change)
    else runs.push({ op: change.op, changes: [change] })
  }
  return runs.map((run, i) => {
    const verb = CHANGE_VERBS[run.op] ?? "Wrote"
    return (
      <RecordChips
        key={`${run.op}:${i}`}
        records={run.changes}
        lead={verb}
        label={`${verb} records`}
      />
    )
  })
}
