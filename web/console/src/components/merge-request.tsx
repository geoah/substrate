/** The merge-request voice, shared by every surface that shows one: the pair
 * (who merges away → who survives) and the matcher's evidence as chips. It
 * lives here rather than on a page because there is no queue page any more —
 * the overview's zone and the request's own detail both read from it. */

import { RecordPeek } from "@/components/record-peek"
import { Badge } from "@/components/ui/badge"
import type { EdgeTarget, SubstrateRecord, KindInfo } from "@/lib/api/types"
import { splitKind } from "@/lib/definition"
import { evidenceSignals, signalText } from "@/lib/mergerequests"

/** The pair, direction first: who merges away → who survives. Each side
 * peeks and links like every edge cell in the console. */
export function MergePair({
  loser,
  winner,
  types,
}: {
  loser?: EdgeTarget
  winner?: EdgeTarget
  types: KindInfo[]
}) {
  return (
    <span className="inline-flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-0.5">
      <PairSide target={loser} types={types} />
      <span className="text-muted-foreground" aria-hidden>
        →
      </span>
      <PairSide target={winner} types={types} />
    </span>
  )
}

function PairSide({
  target,
  types,
}: {
  target?: EdgeTarget
  types: KindInfo[]
}) {
  if (!target) return <span className="text-muted-foreground">unknown</span>
  return (
    <span className="inline-flex min-w-0 items-baseline gap-1.5">
      <span className="min-w-0 truncate font-medium">
        <RecordPeek target={target} types={types} />
      </span>
      <span className="data text-xs text-muted-foreground">
        {splitKind(target.kind).name}
      </span>
    </span>
  )
}

/** The matcher's evidence as honest chips off the real fields. Evidence the
 * console cannot read as signals shows nothing rather than guessing. */
export function EvidenceChips({ mr }: { mr: SubstrateRecord }) {
  const signals = evidenceSignals(mr.properties.evidence)
  if (!signals.length) return null
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {signals.map((s, i) => (
        <Badge
          key={`${s.kind}-${i}`}
          variant="outline"
          className="data font-normal text-muted-foreground"
        >
          {signalText(s)}
        </Badge>
      ))}
    </span>
  )
}
