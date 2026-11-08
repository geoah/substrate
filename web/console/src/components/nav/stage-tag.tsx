/** A place's release stage ("alpha", "beta") as a small tinted pill beside
 * its name: what is still moving under the reader, said where they enter it.
 * Alpha is warm, beta is cool, so the two read apart without the word. The
 * pill is hidden from assistive tech so the row keeps its label as its name;
 * the row carries the stage as its description instead. */

import { stageNote, type Stage } from "@/lib/stage"
import { cn } from "@/lib/utils"

const TONE: Record<Stage, string> = {
  alpha: "bg-kind-orange-bg text-kind-orange-fg",
  beta: "bg-kind-blue-bg text-kind-blue-fg",
}

export function StageTag({
  stage,
  className,
}: {
  stage: Stage
  className?: string
}) {
  return (
    <span
      data-slot="stage-tag"
      aria-hidden
      title={stageNote(stage)}
      className={cn(
        "shrink-0 rounded-full px-1.5 text-[11.5px] leading-4 font-medium tracking-wide lowercase",
        TONE[stage],
        className
      )}
    >
      {stage}
    </span>
  )
}
