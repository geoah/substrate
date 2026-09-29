/** One dispatched tool call, as one compact line: what the agent did in
 * words, and whether it worked. Opening it says what came back: the records a
 * read found, the records a write changed, a sub-agent's reply and what its
 * chain wrote, a function's output, why a call failed. A call with nothing more to say than its check
 * mark does not open. Technical mode names the callable in full, in the actor
 * spelling the rows are stamped with, and adds the request and the response
 * verbatim.
 *
 * A live card and the same card replayed off the records are one component:
 * `ToolCallView` (lib/api/transcript.ts) is filled from the stream while the
 * run is in flight and from the `llm/message` rows afterwards. Only what the
 * reader has to act on renders under the line, open or not: a suggested
 * change and a batch of questions. */

import { useState } from "react"
import { Link } from "@tanstack/react-router"
import {
  CheckIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  HourglassIcon,
  WrenchIcon,
} from "lucide-react"

import { ChangesList } from "@/components/agent/changes"
import { InteractionCard } from "@/components/agent/interaction-card"
import { MessageText } from "@/components/agent/message-text"
import { ProposalCard } from "@/components/agent/proposal-card"
import { CodeBlock } from "@/components/code-block"
import { KindPath } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  callableOf,
  resolveTool,
  toolDetails,
  toolSummary,
  wroteParts,
  wroteSentence,
  type ToolDetail,
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

/** The link into a sub-agent's own thread. */
function ConversationLink({ thread }: { thread: string }) {
  return (
    <Link
      to="/agents"
      search={{ thread } as never}
      className="text-[12px] text-faint underline-offset-2 hover:underline"
    >
      Open its conversation
    </Link>
  )
}

/** What a sub-agent's chain wrote, as one sentence: "Wrote 3 records across
 * task, note". The kinds are words; technical mode names each by its full
 * reference. The entries themselves are in the child thread, which the line
 * links. */
function WroteLine({
  detail,
}: {
  detail: Extract<ToolDetail, { type: "wrote" }>
}) {
  const [technical] = useTechnicalDetails()
  const { records, kinds, moreKinds, thread } = detail
  const parts = wroteParts(records, kinds, moreKinds)
  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {technical ? (
        <span className="min-w-0 [overflow-wrap:anywhere]">
          {parts.lead}
          {parts.kinds.map((kind, i) => (
            <span key={kind}>
              {i === 0 ? " " : ", "}
              <KindPath reference={kind} />
            </span>
          ))}
          {parts.tail}
        </span>
      ) : (
        <span className="min-w-0 [overflow-wrap:anywhere]">
          {wroteSentence(records, kinds, moreKinds)}
        </span>
      )}
      {thread && <ConversationLink thread={thread} />}
    </span>
  )
}

/** What came back, for a reader who does not read JSON. */
function Detail({ detail }: { detail: ToolDetail }) {
  switch (detail.type) {
    case "failed":
      return (
        <span>It didn’t work{detail.reason ? `: ${detail.reason}` : "."}</span>
      )
    case "found":
      if (detail.total === 0) return <span>Nothing matched.</span>
      return (
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>Found</span>
          {detail.records.map((r) => (
            <RecordRef key={`${r.kind}/${r.id}`} kind={r.kind} id={r.id} />
          ))}
          {detail.total > detail.records.length && (
            <span>and {detail.total - detail.records.length} more</span>
          )}
        </span>
      )
    case "changed":
      return <ChangesList changes={detail.changes} />
    case "wrote":
      return <WroteLine detail={detail} />
    case "reply":
      return (
        <div className="flex flex-col gap-1">
          <span className="flex flex-wrap items-center gap-x-2">
            <span>{agentName(detail.agent)} replied</span>
            {detail.thread && <ConversationLink thread={detail.thread} />}
          </span>
          <div className="text-foreground">
            <MessageText text={detail.text} />
          </div>
        </div>
      )
    case "output":
      return <Payload label="What it returned" raw={detail.raw} />
  }
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
  const resolved = resolveTool(agent, call.name, call.callable)
  const summary = toolSummary(call, resolved)
  const running = call.ok === undefined
  const failed = call.ok === false
  // A propose lands a row somebody has to decide: the change is NOT applied
  // until then, so the call carries the suggestion itself.
  const proposed = requestIdOf(call)
  // An ask's interaction renders as the form card, the same live-state rule.
  const asked = interactionIdOf(call)
  // The request and the interaction render as their own cards, so their
  // stamps are not listed again.
  const details = toolDetails(
    call,
    resolved,
    [proposed, asked].filter((id): id is string => Boolean(id))
  )
  // A failed call that LANDED a request was not a failure: the policy held
  // the write for review, and the line says so instead of crying red.
  const held = failed && proposed !== undefined
  const opens = technical || details.length > 0

  const line = (
    <>
      <WrenchIcon className="size-3.5 shrink-0 text-faint" />
      <span className="min-w-0 [overflow-wrap:anywhere]">{summary}</span>
      {running ? (
        <Spinner className="size-3 shrink-0" />
      ) : held ? (
        <span className="inline-flex shrink-0 items-center gap-1 whitespace-nowrap text-warning">
          <HourglassIcon className="size-3.5" />
          Waiting for you
        </span>
      ) : failed ? (
        <span className="inline-flex shrink-0 items-center gap-1 whitespace-nowrap text-destructive">
          <CircleAlertIcon className="size-3.5" />
          Didn’t work
        </span>
      ) : (
        <CheckIcon aria-label="Done" className="size-3.5 shrink-0 text-ok" />
      )}
      {technical && (
        <span className="font-mono text-[11.5px] [overflow-wrap:anywhere] text-faint">
          {callableOf(call, resolved)}
        </span>
      )}
      {opens && (
        <ChevronRightIcon
          className={cn(
            "size-3 shrink-0 text-faint transition-transform",
            open && "rotate-90"
          )}
        />
      )}
    </>
  )
  const lineClass =
    "inline-flex max-w-full items-center gap-2 self-start rounded-lg border bg-background px-2.5 py-1 text-left text-[12.5px] text-muted-foreground"

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {opens ? (
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          className={cn(lineClass, "cursor-pointer hover:bg-hover")}
        >
          {line}
        </button>
      ) : (
        <div className={lineClass}>{line}</div>
      )}
      {open && opens && (
        <div className="flex flex-col gap-2 rounded-lg border bg-panel px-3 py-2 text-[12.5px] text-muted-foreground">
          {details.map((detail, i) => (
            <Detail key={`${detail.type}:${i}`} detail={detail} />
          ))}
          {technical && (
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
          )}
        </div>
      )}
      {proposed && <ProposalCard id={proposed} />}
      {asked && <InteractionCard id={asked} />}
    </div>
  )
}
