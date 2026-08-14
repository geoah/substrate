/** One dispatched tool call, as a card that survives the run: the tool's name,
 * whether it is running / settled / failed, and — on expansion — the REQUEST
 * the model sent and the RESPONSE the dispatch returned, both pretty-printed
 * and tinted. A live card and the same card replayed off the records are the
 * same component: `ToolCallView` (lib/api/transcript.ts) is filled from the
 * stream while the run is in flight and from the `llmmessage` rows afterwards,
 * so nothing disappears when the stream ends. */

import { ChevronRightIcon, WrenchIcon } from "lucide-react"

import { CodeBlock } from "@/components/code-block"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Spinner } from "@/components/ui/spinner"
import type { ToolCallView } from "@/lib/api/transcript"
import { prettyJSON } from "@/lib/code"
import { cn } from "@/lib/utils"

/** A call is RUNNING until something settles it. `ok` is the settled signal —
 * an empty output is a legitimate result, so output alone cannot be. */
function running(call: ToolCallView): boolean {
  return call.ok === undefined
}

function Payload({ label, raw }: { label: string; raw: string }) {
  const { text, json } = prettyJSON(raw)
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[0.65rem] tracking-wide text-muted-foreground uppercase">
        {label}
      </span>
      {text ? (
        json ? (
          <CodeBlock source={text} lang="json" />
        ) : (
          // Not JSON: a tool returns whatever it returns, and tinting a stack
          // trace as if it had parsed would be a lie about the payload.
          <pre className="overflow-x-auto rounded-sm bg-background/60 p-2 data text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
            {text}
          </pre>
        )
      ) : (
        <span className="data text-xs text-muted-foreground">—</span>
      )}
    </div>
  )
}

export function ToolCallCard({ call }: { call: ToolCallView }) {
  const isRunning = running(call)
  const failed = call.ok === false
  return (
    <Collapsible className="group/tool rounded-md border bg-muted/40">
      <CollapsibleTrigger
        className={cn(
          "flex w-full cursor-pointer items-center gap-1.5 px-2 py-1.5 text-left text-xs",
          "hover:bg-muted/60"
        )}
      >
        <WrenchIcon className="size-3 shrink-0 text-muted-foreground" />
        <span className="truncate data">{call.name || "tool"}</span>
        {isRunning ? (
          <Spinner className="size-3 shrink-0 text-muted-foreground" />
        ) : (
          <span
            className={cn(
              "shrink-0 data",
              failed ? "text-destructive" : "text-muted-foreground"
            )}
          >
            {failed ? "failed" : "ok"}
          </span>
        )}
        <ChevronRightIcon className="ml-auto size-3 shrink-0 text-muted-foreground transition-transform group-data-open/tool:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="flex flex-col gap-2 border-t px-2 py-2">
          <Payload label="request" raw={call.arguments} />
          {/* A running call has no response yet — say so, rather than
              rendering an empty block that reads as an empty result. */}
          {isRunning && call.output === undefined ? (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Spinner className="size-3" />
              waiting for the result
            </div>
          ) : (
            <Payload label="response" raw={call.output ?? ""} />
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
