/** One dispatched tool call, as one compact line: what the agent did in
 * words, and whether it worked. Opening it says what came back (the records a
 * read found, why a call failed); technical mode names the function and shows
 * the request and the response verbatim.
 *
 * A live card and the same card replayed off the records are one component:
 * `ToolCallView` (lib/api/transcript.ts) is filled from the stream while the
 * run is in flight and from the `llm/message` rows afterwards. What the call
 * LANDED — a suggested change, a batch of questions, records it changed —
 * renders under the line, where it is the thing the reader acts on. */

import { useState } from "react"
import {
  CheckIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  HourglassIcon,
  WrenchIcon,
} from "lucide-react"

import { ChangesList } from "@/components/agent/changes"
import { InteractionCard } from "@/components/agent/interaction-card"
import { ProposalCard } from "@/components/agent/proposal-card"
import { CodeBlock } from "@/components/code-block"
import { RecordRef } from "@/components/identity/record-ref"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  foundRecords,
  resolveTool,
  toolFailure,
  toolSummary,
} from "@/lib/agent-chat"
import {
  interactionIdOf,
  requestIdOf,
  type ToolCallView,
} from "@/lib/api/transcript"
import type { SubstrateRecord } from "@/lib/api/types"
import { prettyJSON } from "@/lib/code"
import { cn } from "@/lib/utils"

function Payload({ label, raw }: { label: string; raw: string }) {
  const { text, json } = prettyJSON(raw)
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11.5px] font-medium text-faint">{label}</span>
      {text ? (
        json ? (
          <CodeBlock source={text} lang="json" />
        ) : (
          // Not JSON: a tool returns whatever it returns, and tinting a stack
          // trace as if it had parsed would be a lie about the payload.
          <pre className="overflow-x-auto rounded-md bg-background p-2 font-mono text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
            {text}
          </pre>
        )
      ) : (
        <span className="text-xs text-faint">Nothing</span>
      )}
    </div>
  )
}

/** What came back, for a reader who does not read JSON. */
function Outcome({ call }: { call: ToolCallView }) {
  if (call.ok === undefined) {
    return (
      <span className="flex items-center gap-1.5">
        <Spinner className="size-3" />
        Still working on it…
      </span>
    )
  }
  if (call.ok === false) {
    const reason = toolFailure(call.output)
    return <span>It didn’t work{reason ? `: ${reason}` : "."}</span>
  }
  const found = foundRecords(call.output)
  if (found.length > 0) {
    return (
      <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span>Found</span>
        {found.map((r) => (
          <RecordRef key={`${r.kind}/${r.id}`} kind={r.kind} id={r.id} />
        ))}
      </span>
    )
  }
  return <span>It worked.</span>
}

export function ToolCallCard({
  call,
  agent,
}: {
  call: ToolCallView
  /** The agent that made the call, whose `tools:` say what the name means. */
  agent?: SubstrateRecord
}) {
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState(false)
  const resolved = resolveTool(agent, call.name)
  const summary = toolSummary(call, resolved)
  const running = call.ok === undefined
  const failed = call.ok === false
  // A propose lands a row somebody has to decide: the change is NOT applied
  // until then, so the call carries the suggestion itself.
  const proposed = requestIdOf(call)
  // An ask's interaction renders as the form card, the same live-state rule.
  const asked = interactionIdOf(call)
  // The dispatch's other writes; the request and the interaction already
  // render as their own cards.
  const changes = (call.changes ?? []).filter(
    (c) => c.id !== proposed && c.id !== asked
  )
  // A failed call that LANDED a request was not a failure: the policy held
  // the write for review, and the line says so instead of crying red.
  const held = failed && proposed !== undefined

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="inline-flex max-w-full cursor-pointer flex-wrap items-center gap-2 self-start rounded-lg border bg-background px-2.5 py-1 text-left text-[12.5px] text-muted-foreground hover:bg-hover"
      >
        <WrenchIcon className="size-3.5 shrink-0 text-faint" />
        <span className="min-w-0 [overflow-wrap:anywhere]">{summary}</span>
        {running ? (
          <Spinner className="size-3 shrink-0" />
        ) : held ? (
          <span className="inline-flex items-center gap-1 text-warning">
            <HourglassIcon className="size-3.5" />
            Waiting for you
          </span>
        ) : failed ? (
          <span className="inline-flex items-center gap-1 text-destructive">
            <CircleAlertIcon className="size-3.5" />
            Didn’t work
          </span>
        ) : (
          <CheckIcon aria-label="Done" className="size-3.5 shrink-0 text-ok" />
        )}
        {technical && (
          <span className="font-mono text-[11px] [overflow-wrap:anywhere] text-faint">
            {resolved.function ?? resolved.subagent ?? call.name}
          </span>
        )}
        <ChevronRightIcon
          className={cn(
            "size-3 shrink-0 text-faint transition-transform",
            open && "rotate-90"
          )}
        />
      </button>
      {open && (
        <div className="flex flex-col gap-2 rounded-lg border bg-panel px-3 py-2 text-[12.5px] text-muted-foreground">
          {technical ? (
            <>
              <Payload label="Request" raw={call.arguments} />
              {running && call.output === undefined ? (
                <span className="flex items-center gap-1.5 text-xs">
                  <Spinner className="size-3" />
                  Waiting for the result
                </span>
              ) : (
                <Payload label="Response" raw={call.output ?? ""} />
              )}
            </>
          ) : (
            <Outcome call={call} />
          )}
        </div>
      )}
      {changes.length > 0 && <ChangesList changes={changes} />}
      {proposed && <ProposalCard id={proposed} />}
      {asked && <InteractionCard id={asked} />}
    </div>
  )
}
