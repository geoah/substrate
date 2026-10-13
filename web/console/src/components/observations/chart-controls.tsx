/** The controls a timeline and the streams page's Timeline panel share:
 * the bucket (Automatic naming the size it picked) and what to group the
 * series by, each a labelled row of choices. */

import { Labelled } from "@/components/observations/picker"
import { Segmented } from "@/components/ui/segmented"
import type { TimelineControls } from "@/hooks/use-timeline-controls"
import { GROUP_CHOICES, type GroupChoice } from "@/lib/observations-reads"
import {
  BUCKET_CHOICES,
  BUCKET_SIZE_WORDS,
  type BucketChoice,
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

export function GroupControl({
  value,
  onChange,
}: {
  value: GroupChoice
  onChange: (next: GroupChoice) => void
}) {
  return (
    <Labelled label="Group by">
      <Segmented
        label="Group by"
        value={value}
        options={GROUP_CHOICES}
        onChange={onChange}
      />
    </Labelled>
  )
}
