/** A metric record's rollups (core's `metric` trait): the tiers the
 * observed index keeps for it, the default where it lists none, and how to
 * change them. The value is edited where every property is, in the sheet
 * below; this says what it means. */

import { SectionHead } from "@/components/identity/section-head"
import type { SubstrateRecord } from "@/lib/api/types"
import { ROLLUP_TIERS } from "@/lib/aggregate-rules"
import { rollupWords } from "@/lib/observations"
import { listWords } from "@/lib/tools"

export function MetricRollups({
  record,
  readOnly,
}: {
  record: SubstrateRecord
  readOnly: boolean
}) {
  return (
    <section data-slot="metric-rollups" aria-label="Rollups">
      <SectionHead title="Rollups" hint="the summaries the index keeps" />
      <p className="text-[13.5px]">{rollupWords(record.properties?.rollups)}</p>
      <p className="mt-1 text-[12.5px] text-muted-foreground">
        Each tier keeps a summary row of this metric’s values per window of that
        width, so a long read takes those rows instead of every point. A metric
        may keep {listWords([...ROLLUP_TIERS])}, finest first.
        {readOnly
          ? ""
          : " Change them under Rollups in the properties below; a change re-derives this metric’s rollups in the background."}
      </p>
    </section>
  )
}
