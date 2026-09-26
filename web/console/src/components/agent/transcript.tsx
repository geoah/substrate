/** The conversation itself: your messages as bubbles on the right, the
 * agent's turns on the left under its mark — what it did as compact tool
 * lines, what it suggested as cards, what it said as text — and a caret on
 * the turn still arriving. Everything here renders `TurnView`s, so the live
 * run and the reloaded thread are one render path.
 *
 * Consecutive assistant turns are ONE reply to the reader: a loop that reads,
 * then suggests, then answers is three turns on the wire and one message on
 * screen. Within a turn, its tool calls sit above its text, because one
 * completion produces both and the text is what follows the calls. */

import { CheckCircle2Icon, XCircleIcon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { ChangesList } from "@/components/agent/changes"
import { MessageText } from "@/components/agent/message-text"
import { ToolCallCard } from "@/components/agent/tool-call"
import { TriggerContext } from "@/components/agent/trigger-context"
import { RecordRef } from "@/components/identity/record-ref"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  decisionNoticeOf,
  deliveryNoticeOf,
  interactionNoticeOf,
  type TurnView,
} from "@/lib/api/transcript"
import type { SubstrateRecord } from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"

/** The substrate's own turn: a decision or an answer as one quiet line, or —
 * for a system row this reader cannot decode — the raw content, so nothing in
 * a thread is ever silently dropped. */
function SystemTurn({ turn }: { turn: TurnView }) {
  const [technical] = useTechnicalDetails()
  const notice = decisionNoticeOf(turn)
  const interaction = interactionNoticeOf(turn)
  const target = notice?.target ? splitRecordPath(notice.target) : undefined
  const positive = interaction
    ? interaction.event === "interactionAnswered"
    : notice?.decision === "accepted"
  const Icon = positive ? CheckCircle2Icon : XCircleIcon
  return (
    <div className="flex flex-col items-center gap-1.5 text-[12.5px] text-muted-foreground">
      {interaction || notice ? (
        <div className="flex flex-wrap items-center justify-center gap-1.5">
          <Icon
            className={positive ? "size-3.5 text-ok" : "size-3.5 text-faint"}
          />
          {interaction ? (
            <span>
              You{" "}
              {interaction.event === "interactionAnswered"
                ? "answered its questions"
                : "dismissed its questions"}
              {interaction.answers &&
                `: ${interaction.answers.map((a) => a.selected.join(", ")).join("; ")}`}
            </span>
          ) : notice ? (
            <>
              <span>
                You{" "}
                {notice.decision === "accepted"
                  ? notice.deleted
                    ? "applied the deletion of"
                    : "applied the change to"
                  : "dismissed the change to"}
              </span>
              {target ? (
                <RecordRef kind={target.kind} id={target.id} />
              ) : (
                <span>a record</span>
              )}
              {technical && notice.version !== undefined && (
                <span className="font-mono text-[11.5px] text-faint">
                  v{notice.version}
                </span>
              )}
            </>
          ) : null}
        </div>
      ) : (
        <p className="max-w-[85%] font-mono text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
          {turn.content}
        </p>
      )}
      {technical && turn.changes && turn.changes.length > 0 && (
        <ChangesList changes={turn.changes} />
      )}
    </div>
  )
}

function UserTurn({ turn }: { turn: TurnView }) {
  return (
    <div
      data-slot="user-message"
      className="max-w-[70%] self-end rounded-[14px_14px_4px_14px] bg-hover px-3.5 py-2 leading-relaxed [overflow-wrap:anywhere] whitespace-pre-wrap"
    >
      {turn.content}
    </div>
  )
}

/** One agent reply: the run of assistant turns between two of yours. */
function AgentReply({
  turns,
  agentId,
  agent,
  liveKey,
}: {
  turns: TurnView[]
  agentId?: string
  agent?: SubstrateRecord
  liveKey?: string
}) {
  return (
    <div
      data-slot="agent-message"
      className="grid max-w-[680px] grid-cols-[26px_minmax(0,1fr)] gap-2.5 leading-relaxed"
    >
      <span className="mt-0.5">
        {agentId && <AgentMark id={agentId} size="md" />}
      </span>
      <div className="flex min-w-0 flex-col gap-2.5">
        {turns.map((turn) => (
          <div key={turn.key} className="flex min-w-0 flex-col gap-2.5">
            {turn.tools.map((call, i) => (
              <ToolCallCard
                key={call.id || `${turn.key}:${i}`}
                call={call}
                agent={agent}
              />
            ))}
            {(turn.content || turn.key === liveKey) && (
              <div>
                {turn.content && <MessageText text={turn.content} />}
                {turn.key === liveKey && (
                  <span
                    aria-label="Still writing"
                    className="ml-0.5 inline-block h-3.5 w-1 animate-pulse bg-current align-middle"
                  />
                )}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

type Group =
  | { type: "user"; turn: TurnView }
  | { type: "system"; turn: TurnView }
  | { type: "trigger"; turn: TurnView }
  | { type: "agent"; key: string; turns: TurnView[] }

function groupTurns(turns: TurnView[]): Group[] {
  const out: Group[] = []
  turns.forEach((turn, i) => {
    // A triggered thread opens with a delivery envelope as its first user
    // turn; only the FIRST is read that way — a later user message that
    // happens to be JSON is a message somebody sent.
    if (i === 0 && turn.role === "user" && deliveryNoticeOf(turn)) {
      out.push({ type: "trigger", turn })
      return
    }
    if (turn.role === "user") {
      out.push({ type: "user", turn })
      return
    }
    if (turn.role === "system") {
      out.push({ type: "system", turn })
      return
    }
    const last = out[out.length - 1]
    if (last?.type === "agent") last.turns.push(turn)
    else out.push({ type: "agent", key: turn.key, turns: [turn] })
  })
  return out
}

export function Transcript({
  turns,
  liveKey,
  agentId,
  agent,
}: {
  turns: TurnView[]
  /** The turn still arriving, by key — never "the last one", which between a
   * send and the first delta is the PREVIOUS run's settled answer. */
  liveKey?: string
  /** The agent's record id, for its mark. */
  agentId?: string
  /** The agent's record, whose `tools:` say what each call's name means. */
  agent?: SubstrateRecord
}) {
  return (
    <>
      {groupTurns(turns).map((group) => {
        switch (group.type) {
          case "trigger":
            return (
              <TriggerContext
                key={group.turn.key}
                turn={group.turn}
                notice={deliveryNoticeOf(group.turn)!}
              />
            )
          case "user":
            return <UserTurn key={group.turn.key} turn={group.turn} />
          case "system":
            return <SystemTurn key={group.turn.key} turn={group.turn} />
          default:
            return (
              <AgentReply
                key={group.key}
                turns={group.turns}
                agentId={agentId}
                agent={agent}
                liveKey={liveKey}
              />
            )
        }
      })}
    </>
  )
}
