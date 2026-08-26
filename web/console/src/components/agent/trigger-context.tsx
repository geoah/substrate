/** A triggered thread's opening turn, readable: what fired (the change's op,
 * kind, seq and actor) and which record was delivered, as the pill every other
 * surface uses for a record reference. The raw envelope stays one click away —
 * it is what the model actually read, so hiding it entirely would misreport
 * the prompt — but a page of JSON is not how a reader learns why a run exists. */

import { ChevronRightIcon, ZapIcon } from "lucide-react"

import { CodeBlock } from "@/components/code-block"
import { RecordPill } from "@/components/record-pill"
import { Badge } from "@/components/ui/badge"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import type { DeliveryNotice, TurnView } from "@/lib/api/transcript"
import { prettyJSON } from "@/lib/code"

export function TriggerContext({
  turn,
  notice,
}: {
  /** The user turn carrying the envelope; its content is the raw view. */
  turn: TurnView
  notice: DeliveryNotice
}) {
  const { change, record } = notice
  const raw = prettyJSON(turn.content)
  return (
    <Collapsible className="group/trigger rounded-md border bg-muted/40">
      <div className="flex flex-col gap-1.5 px-3 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
          <ZapIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="text-muted-foreground">Triggered by</span>
          <Badge variant="outline" className="data font-normal">
            {change.op}
          </Badge>
          <span className="text-muted-foreground">on</span>
          <RecordPill kind={record.kind} id={record.id} title={record.title} />
        </div>
        <p className="data text-[0.7rem] [overflow-wrap:anywhere] text-muted-foreground">
          {change.kind}/{change.id}
          {change.seq !== undefined && `, changelog seq ${change.seq}`}
          {change.actor && `, by ${change.actor}`}
        </p>
        <CollapsibleTrigger className="flex w-fit cursor-pointer items-center gap-1 text-[0.7rem] text-muted-foreground hover:text-foreground">
          <ChevronRightIcon className="size-3 shrink-0 transition-transform group-data-open/trigger:rotate-90" />
          raw envelope
        </CollapsibleTrigger>
      </div>
      <CollapsibleContent>
        <div className="border-t px-3 py-2">
          <CodeBlock
            source={raw.text}
            lang="json"
            className="[overflow-wrap:anywhere] whitespace-pre-wrap"
          />
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
