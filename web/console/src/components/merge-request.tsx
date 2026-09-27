/** A merge request's evidence: the matcher's signals, each a chip. */

import { Pill } from "@/components/identity/pill"
import type { SubstrateRecord } from "@/lib/api/types"
import { evidenceSignals, signalText } from "@/lib/mergerequests"

/** The matcher's evidence as honest chips off the real fields. Evidence the
 * console cannot read as signals shows nothing rather than guessing. */
export function EvidenceChips({ mr }: { mr: SubstrateRecord }) {
  const signals = evidenceSignals(mr.properties.evidence)
  if (!signals.length) return null
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {signals.map((s, i) => (
        <Pill key={`${s.kind}-${i}`} tone="neutral" dot={false}>
          {signalText(s)}
        </Pill>
      ))}
    </span>
  )
}
