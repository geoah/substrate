/** The conversation itself: user turns as bubbles, assistant turns as their
 * tool calls and their prose, and a caret on the turn still arriving.
 * Everything here renders `TurnView`s, so the live run and the reloaded thread
 * are one render path.
 *
 * Cards sit ABOVE the text of their own turn, though one completion produces
 * both: what the model said before deciding to call something reads as the
 * lead-in to the calls, and the reply that follows is the next turn's. */

import { CheckCircle2Icon, XCircleIcon } from "lucide-react"

import { ChangesList } from "@/components/agent/changes"
import { ToolCallCard } from "@/components/agent/tool-call"
import {
  decisionNoticeOf,
  interactionNoticeOf,
  type TurnView,
} from "@/lib/api/transcript"
import { cn } from "@/lib/utils"

/** The substrate's own turn: a decision notice rendered as an event line, or
 * — for a system row this reader cannot decode — the raw content, still
 * labelled `system` so nothing in a thread is ever silently dropped. */
function SystemTurn({ turn }: { turn: TurnView }) {
  const notice = decisionNoticeOf(turn)
  const interaction = interactionNoticeOf(turn)
  return (
    <div className="flex flex-col items-center gap-1.5">
      <div className="flex max-w-[85%] flex-col gap-1.5 rounded-md border border-dashed bg-muted/30 px-3 py-2">
        {interaction ? (
          <div className="flex flex-col gap-1 text-xs">
            <div className="flex items-center gap-1.5">
              {interaction.event === "interactionAnswered" ? (
                <CheckCircle2Icon className="size-3.5 shrink-0 text-emerald-600" />
              ) : (
                <XCircleIcon className="size-3.5 shrink-0 text-muted-foreground" />
              )}
              <span>
                Questions{" "}
                {interaction.event === "interactionAnswered"
                  ? "answered"
                  : "dismissed"}
              </span>
            </div>
            {interaction.answers && (
              <ul className="flex flex-col gap-0.5 pl-5">
                {interaction.answers.map((a) => (
                  <li key={a.question} className="[overflow-wrap:anywhere]">
                    <span className="data">{a.question}</span>:{" "}
                    <span className="data">{a.selected.join(", ")}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : notice ? (
          <div className="flex items-center gap-1.5 text-xs">
            {notice.decision === "accepted" ? (
              <CheckCircle2Icon className="size-3.5 shrink-0 text-emerald-600" />
            ) : (
              <XCircleIcon className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span>
              Proposal {notice.decision}
              {notice.target && (
                <>
                  : <span className="data">{notice.op}</span> on{" "}
                  <span className="data">{notice.target}</span>
                </>
              )}
              {notice.version !== undefined && (
                <>
                  , now <span className="data">v{notice.version}</span>
                </>
              )}
              {notice.deleted && ", deleted"}
            </span>
          </div>
        ) : (
          <p className="data text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
            {turn.content}
          </p>
        )}
        {turn.changes && turn.changes.length > 0 && (
          <ChangesList changes={turn.changes} />
        )}
      </div>
    </div>
  )
}

export function Turn({ turn, live }: { turn: TurnView; live?: boolean }) {
  if (turn.role === "system") return <SystemTurn turn={turn} />
  const isUser = turn.role === "user"
  return (
    <div
      className={cn(
        "flex flex-col gap-1.5",
        isUser ? "items-end" : "items-start"
      )}
    >
      <span className="px-1 text-[0.65rem] tracking-wide text-muted-foreground uppercase">
        {turn.role}
      </span>
      {turn.tools.length > 0 && (
        <div className="flex w-full max-w-[85%] flex-col gap-1">
          {turn.tools.map((call, i) => (
            <ToolCallCard key={call.id || `${turn.key}:${i}`} call={call} />
          ))}
        </div>
      )}
      {(turn.content || live) && (
        <div
          className={cn(
            "max-w-[85%] rounded-md px-3 py-2 text-sm [overflow-wrap:anywhere] whitespace-pre-wrap",
            isUser ? "bg-primary text-primary-foreground" : "border bg-muted/40"
          )}
        >
          {turn.content}
          {live && (
            <span className="ml-0.5 inline-block h-3.5 w-1 animate-pulse bg-current align-middle" />
          )}
        </div>
      )}
    </div>
  )
}

export function Transcript({
  turns,
  liveKey,
}: {
  turns: TurnView[]
  /** The turn still arriving, by key — never "the last one", which between a
   * send and the first delta is the PREVIOUS run's settled answer. */
  liveKey?: string
}) {
  return (
    <>
      {turns.map((turn) => (
        <Turn key={turn.key} turn={turn} live={turn.key === liveKey} />
      ))}
    </>
  )
}
