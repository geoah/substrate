/** The controls a timeline and the streams page's Timeline panel share:
 * the bucket (Automatic naming the size it picked), where a day starts, and
 * what to group the series by, each a labelled row of choices. */

import { Labelled } from "@/components/observations/picker"
import { Segmented } from "@/components/ui/segmented"
import type { TimelineControls } from "@/hooks/use-timeline-controls"
import { GROUP_CHOICES, type GroupChoice } from "@/lib/observations-reads"
import {
  BUCKET_CHOICES,
  BUCKET_SIZE_WORDS,
  dayBucket,
  type BucketChoice,
  type DayBoundary,
} from "@/lib/observations-time"

export function BucketControl({
  controls,
}: {
  controls: Pick<TimelineControls, "bucket" | "size" | "onBucket">
}) {
  const { bucket, size } = controls
  return (
    <Labelled label="Bucket">
      <Segmented
        label="Bucket"
        value={bucket}
        options={BUCKET_CHOICES.map((b) => ({
          value: b.value,
          label:
            b.value === "auto" && bucket === "auto"
              ? `Automatic: ${BUCKET_SIZE_WORDS[size]}`
              : b.label,
        }))}
        onChange={(next: BucketChoice) => controls.onBucket(next)}
      />
    </Labelled>
  )
}

/** Where a day, week or month bucket starts: where each value was recorded
 * (a day in London is London's day), or the reader's zone. Shown only for
 * those sizes; finer buckets are instants. */
export function DayControl({
  controls,
}: {
  controls: Pick<TimelineControls, "size" | "days" | "timezone" | "onDays">
}) {
  if (!dayBucket(controls.size)) return null
  return (
    <Labelled label="Days">
      <Segmented
        label="Days"
        value={controls.days}
        options={[
          { value: "local", label: "Where I was" },
          { value: "zone", label: controls.timezone },
        ]}
        onChange={(next: DayBoundary) => controls.onDays(next)}
      />
    </Labelled>
  )
}

/** What to group the series by. Where every metric drawn is a category,
 * whose stacks are always its codes, the choice does nothing: `fixed` says
 * why, and the control shows Metric, disabled, beside the reason. */
export function GroupControl({
  value,
  onChange,
  fixed,
}: {
  value: GroupChoice
  onChange: (next: GroupChoice) => void
  fixed?: string
}) {
  return (
    <Labelled label="Group by">
      <Segmented
        label="Group by"
        value={fixed ? "metric" : value}
        options={GROUP_CHOICES}
        onChange={onChange}
        disabled={Boolean(fixed)}
      />
      {fixed && <span className="text-[12.5px] text-faint">{fixed}</span>}
    </Labelled>
  )
}
