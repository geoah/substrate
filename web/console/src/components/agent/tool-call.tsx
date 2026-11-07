/** One dispatched tool call, as one compact line: what the agent did in
 * words, and whether it worked. Opening it says what came back: the records a
 * read found, the records a write changed, a sub-agent's reply and what its
 * chain wrote, a function's output, why a call failed. A call with nothing more to say than its check
 * mark does not open. Technical mode names the callable in full, in the actor
 * spelling the rows are stamped with, and adds the request and the response
 * verbatim.
 *
 * A call that worked on one record names it in its line ("Changed the
 * description of “Dune”"); one that worked on several lists the first few
 * under the line as chips that open them, without opening the card.
 *
 * A live card and the same card replayed off the records are one component:
 * `ToolCallView` (lib/api/transcript.ts) is filled from the stream while the
 * run is in flight and from the `llm/message` rows afterwards. What the
 * reader has to act on renders under the line too, open or not: a suggested
 * change and a batch of questions. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
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
import { ChangeChips, RecordChips } from "@/components/agent/tool-records"
import { ToolResultView } from "@/components/agent/tool-views"
import { CodeBlock } from "@/components/code-block"
import { KindPath } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useHasQueryClient } from "@/hooks/use-has-query-client"
import {
  agentName,
  callableOf,
  resolveTool,
  toolDetails,
  toolSubject,
  toolSummary,
  wroteParts,
  wroteSentence,
  type ResolvedTool,
  type ToolDetail,
  type ToolSubject,
} from "@/lib/agent-chat"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { kindByIdentity } from "@/lib/definition"
import { recordTitleQueryOptions } from "@/lib/reference-titles"
import {
  interactionIdOf,
  requestIdOf,
  type ToolCallView,
} from "@/lib/api/transcript"
import type { SubstrateRecord } from "@/lib/api/types"
import { prettyJSON } from "@/lib/code"
import { HOST_FUNCTIONS } from "@/lib/agent-grants"
import { toolResult } from "@/lib/tool-views"
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
      search={{ thread }}
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
  const resolved = resolveTool(agent, call.name, call.callable)
  const subject = toolSubject(call, resolved)
  const client = useHasQueryClient()
  // Held here: the card below switches component when its answer lands
  // untitled, and a card the reader opened stays open across the switch.
  const [open, setOpen] = useState(false)
  // The line names the one record the call worked on; an answer that did
  // not carry its heading (an edit, a delete) has it read, as a mark does.
  if (subject && !subject.title && client) {
    return (
      <TitledToolCall
        call={call}
        resolved={resolved}
        subject={subject}
        open={open}
        onOpen={setOpen}
      />
    )
  }
  return (
    <ToolCall call={call} resolved={resolved} open={open} onOpen={setOpen} />
  )
}

function TitledToolCall({
  subject,
  ...rest
}: {
  call: ToolCallView
  resolved: ResolvedTool
  subject: ToolSubject
  open: boolean
  onOpen: (open: boolean) => void
}) {
  const kinds = useQuery(kindsQueryOptions)
  // A kind the repository never declared refuses the batched read.
  const known = Boolean(kindByIdentity(kinds.data ?? [], subject.kind))
  const title = useQuery({
    ...recordTitleQueryOptions(subject.kind, subject.id),
    enabled: known,
  })
  return <ToolCall {...rest} title={title.data ?? undefined} />
}

function ToolCall({
  call,
  resolved,
  title,
  open,
  onOpen,
}: {
  call: ToolCallView
  resolved: ResolvedTool
  /** The heading of the one record the call worked on, read for it. */
  title?: string
  open: boolean
  onOpen: (open: boolean) => void
}) {
  const [technical] = useTechnicalDetails()
  const summary = toolSummary(call, { ...resolved, title })
  // A background job still working reads as a running call: its handle is
  // not its result.
  const working = call.job === "working"
  const running = call.ok === undefined || working
  const failed = call.ok === false
  // A propose lands a row somebody has to decide: the change is NOT applied
  // until then, so the call carries the suggestion itself.
  const proposed = requestIdOf(call)
  // An ask's interaction renders as the form card, the same live-state rule.
  const asked = interactionIdOf(call)
  // A function whose result has a shape the console knows draws it under
  // the line, so its payload is not shown again as raw output. The host
  // functions and sub-agents have details of their own.
  const known =
    resolved.function && !HOST_FUNCTIONS.includes(resolved.function)
      ? toolResult(call)
      : undefined
  // The request and the interaction render as their own cards, so their
  // stamps are not listed again.
  const all = toolDetails(
    call,
    resolved,
    [proposed, asked].filter((id): id is string => Boolean(id))
  ).filter((detail) => !(known && detail.type === "output"))
  // Several records, and the changes of a call whose line says something
  // else (a function's own words), show under the line; one record the line
  // already names keeps its link in the details.
  const host = HOST_FUNCTIONS.includes(resolved.function ?? "")
  const inline = all.filter(
    (detail) =>
      (detail.type === "found" && detail.records.length > 1) ||
      (detail.type === "changed" &&
        (detail.changes.length > 1 || (!host && !resolved.subagent)))
  )
  const details = all.filter((detail) => !inline.includes(detail))
  // A failed call that LANDED a request was not a failure: the policy held
  // the write for review, and the line says so instead of crying red.
  const held = failed && proposed !== undefined
  const opens = technical || details.length > 0

  const line = (
    <>
      <WrenchIcon className="size-3.5 shrink-0 text-faint" />
      <span className="min-w-0 [overflow-wrap:anywhere]">{summary}</span>
      {working ? (
        <span className="inline-flex shrink-0 items-center gap-1 whitespace-nowrap">
          <Spinner className="size-3" />
          Working in the background
        </span>
      ) : running ? (
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
          onClick={() => onOpen(!open)}
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
      {known && (
        <div className="pl-1">
          <ToolResultView result={known} />
        </div>
      )}
      {inline.map((detail, i) =>
        detail.type === "found" ? (
          <RecordChips
            key={`found:${i}`}
            records={detail.records}
            total={detail.total}
            label="Records found"
          />
        ) : detail.type === "changed" ? (
          <ChangeChips key={`changed:${i}`} changes={detail.changes} />
        ) : null
      )}
      {proposed && <ProposalCard id={proposed} />}
      {asked && <InteractionCard id={asked} />}
    </div>
  )
}
