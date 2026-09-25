/** A triggered thread's opening turn, readable: the record whose change
 * started the run, as its mark, in one sentence. Technical mode adds what
 * fired (the op, the change's address, its seq and actor) and the raw
 * envelope — it is what the model actually read, so it stays reachable. */

import { ChevronRightIcon, ZapIcon } from "lucide-react"

import { CodeBlock } from "@/components/code-block"
import { RecordRef } from "@/components/identity/record-ref"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { DeliveryNotice, TurnView } from "@/lib/api/transcript"
import { prettyJSON } from "@/lib/code"

const HAPPENED: Record<string, string> = {
  create: "was added",
  put: "was saved",
  update: "changed",
  patch: "changed",
  delete: "was deleted",
}

export function TriggerContext({
  turn,
  notice,
}: {
  /** The user turn carrying the envelope; its content is the raw view. */
  turn: TurnView
  notice: DeliveryNotice
}) {
  const [technical] = useTechnicalDetails()
  const { change, record } = notice
  const raw = prettyJSON(turn.content)
  return (
    <Collapsible className="group/trigger flex flex-col items-center gap-1.5 text-[12.5px] text-muted-foreground">
      <div className="flex flex-wrap items-center justify-center gap-1.5">
        <ZapIcon className="size-3.5 shrink-0 text-faint" />
        <span>Started on its own because</span>
        <RecordRef kind={record.kind} id={record.id} title={record.title} />
        <span>{HAPPENED[change.op] ?? "changed"}</span>
      </div>
      {technical && (
        <>
          <p className="font-mono text-[11px] [overflow-wrap:anywhere] text-faint">
            <span>{change.op}</span> {change.kind}/{change.id}
            {change.seq !== undefined && `, changelog seq ${change.seq}`}
            {change.actor && `, by ${change.actor}`}
          </p>
          <CollapsibleTrigger className="flex w-fit cursor-pointer items-center gap-1 text-[11.5px] hover:text-foreground">
            <ChevronRightIcon className="size-3 shrink-0 transition-transform group-data-open/trigger:rotate-90" />
            Raw envelope
          </CollapsibleTrigger>
          <CollapsibleContent className="w-full">
            <CodeBlock
              source={raw.text}
              lang="json"
              className="[overflow-wrap:anywhere] whitespace-pre-wrap"
            />
          </CollapsibleContent>
        </>
      )}
    </Collapsible>
  )
}
