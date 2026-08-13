/** The conversation itself: user turns as bubbles, assistant turns as prose
 * with their tool calls above the text (the loop dispatches, then answers), and
 * a caret on the turn still streaming. Everything here renders `TurnView`s, so
 * the live run and the reloaded thread are one render path. */

import { ToolCallCard } from "@/components/agent/tool-call"
import type { TurnView } from "@/lib/api/transcript"
import { cn } from "@/lib/utils"

export function Turn({ turn, live }: { turn: TurnView; live?: boolean }) {
  const isUser = turn.role === "user"
  return (
    <div className={cn("flex flex-col gap-1.5", isUser ? "items-end" : "items-start")}>
      <span className="px-1 text-[0.65rem] uppercase tracking-wide text-muted-foreground">
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
            "max-w-[85%] rounded-md px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]",
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
  streaming,
}: {
  turns: TurnView[]
  /** Marks the last assistant turn as still arriving. */
  streaming?: boolean
}) {
  const lastAssistant = streaming
    ? turns.map((t) => t.role).lastIndexOf("assistant")
    : -1
  return (
    <>
      {turns.map((turn, i) => (
        <Turn key={turn.key} turn={turn} live={i === lastAssistant} />
      ))}
    </>
  )
}
