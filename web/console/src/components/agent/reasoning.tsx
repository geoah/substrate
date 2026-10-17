/** The model's thinking on one assistant turn: a quiet line that says how
 * long it thought, with the text behind it, folded until asked for. While the
 * turn is still thinking the line counts the seconds. The text is plain: it
 * is the model talking to itself, so it is shown as written and never read
 * as markdown or as record paths. */

import { BrainIcon, ChevronRightIcon } from "lucide-react"
import { useEffect, useState } from "react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import type { TurnView } from "@/lib/api/transcript"
import { thinkingLabel } from "@/lib/thinking-time"

export function Reasoning({ turn }: { turn: TurnView }) {
  const live = turn.reasoningStartedAt !== undefined
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!live) return
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [live])
  if (!turn.reasoning) return null
  return (
    <Collapsible
      data-slot="reasoning"
      className="group/reasoning flex flex-col gap-1.5 text-[12.5px] text-muted-foreground"
    >
      <CollapsibleTrigger className="flex w-fit cursor-pointer items-center gap-1.5 hover:text-foreground">
        <BrainIcon className="size-3.5 shrink-0 text-faint" />
        <span>{thinkingLabel(turn, live ? now : 0)}</span>
        <ChevronRightIcon className="size-3.5 shrink-0 text-faint transition-transform group-data-open/reasoning:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="border-l-2 pl-3 leading-relaxed [overflow-wrap:anywhere] whitespace-pre-wrap">
          {turn.reasoning}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
