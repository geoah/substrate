/** The conversation column: the header, the thread's messages, and the
 * composer. A thread IS a run, and the transcript IS records — `llm/thread`
 * and `llm/message` rows the loop writes as it goes — so this reads them back
 * and nothing shown while streaming is lost on reload.
 *
 * TWO SOURCES, ONE RENDER PATH. The persisted rows are the truth; the live
 * ndjson stream is an overlay on top of them, folded into the same `TurnView`
 * shape (lib/api/transcript.ts). The overlay exists because the records query
 * is deliberately NOT read mid-run — a partial read would double every turn
 * the stream is already showing.
 *
 * The handover is the delicate part. When a run settles the rows are marked
 * stale and the query re-enables; the overlay is dropped only once that fetch
 * has LANDED, watched through the query itself. Awaiting the invalidation
 * instead does not work — `invalidateQueries` will not refetch a disabled
 * query, so the await returns before the rows it is waiting for exist, and the
 * conversation blinks back to its pre-run state for a round trip.
 *
 * The parent keys this component by conversation, and keeps the key when a
 * new chat's run mints its thread — remounting then would drop the stream. */

import { useEffect, useRef, useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { KeyRoundIcon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { Composer } from "@/components/agent/composer"
import { Transcript } from "@/components/agent/transcript"
import { IdText } from "@/components/identity/id-text"
import { Button } from "@/components/ui/button"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  examplePrompts,
  providerName,
  tallyWords,
  threadTally,
} from "@/lib/agent-chat"
import {
  streamChat,
  threadMessagesQueryOptions,
  transcriptOrder,
  type AgentEvent,
  type AgentResult,
  type ChatHandle,
} from "@/lib/api/agents"
import { CORE_AUTHORITY, LLM_PACKAGE, LLM_PACKAGE_NAME } from "@/lib/api/http"
import {
  EMPTY_OVERLAY,
  pushDelta,
  pushToolStart,
  settleTool,
  transcriptOf,
  type LiveOverlay,
} from "@/lib/api/transcript"
import type { SubstrateRecord } from "@/lib/api/types"

/** The callout for an agent whose provider row has no key: the run would be
 * refused at dispatch, so the page says why before anybody types. */
function KeylessCallout({ provider }: { provider: string }) {
  return (
    <div
      role="note"
      className="flex items-start gap-2.5 rounded-lg border border-warning/30 bg-warn-soft px-3.5 py-2.5 text-[13px]"
    >
      <KeyRoundIcon className="mt-0.5 size-4 shrink-0 text-warning" />
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">
          This agent can’t answer yet: add an API key for{" "}
          {providerName(provider)}
        </span>
        <span className="text-muted-foreground">
          Agents run on your own key, kept on the provider’s record.{" "}
          <Link
            to="/data/$authority/$pkg/$name/$id"
            params={{
              authority: CORE_AUTHORITY,
              pkg: LLM_PACKAGE_NAME,
              name: "provider",
              id: provider,
            }}
            className="text-primary-text underline underline-offset-2"
          >
            Add the key
          </Link>
        </span>
      </div>
    </div>
  )
}

export function Conversation({
  agentId,
  agent,
  thread,
  threadRecord,
  title,
  onThread,
  draft,
  onDraft,
  pickable,
  onPickAgent,
  keylessProvider,
  actions,
}: {
  /** The agent this conversation is with; undefined while none is known. */
  agentId?: string
  agent?: SubstrateRecord
  /** The thread being read; empty for a new chat. */
  thread: string
  /** Its record, once read: technical mode prints its stored status and
   * tally. */
  threadRecord?: SubstrateRecord
  title: string
  /** A new chat's run named its thread. */
  onThread: (thread: string) => void
  draft: string
  onDraft: (draft: string) => void
  /** The agents a NEW chat may go to; an existing thread keeps its own. */
  pickable?: string[]
  onPickAgent?: (agent: string) => void
  /** The provider row id, when the agent's provider has no key. */
  keylessProvider?: string
  /** The header's buttons. */
  actions?: ReactNode
}) {
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  const [live, setLive] = useState<LiveOverlay>(EMPTY_OVERLAY)
  const [streaming, setStreaming] = useState(false)
  const [result, setResult] = useState<AgentResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  // The thread this conversation's own run minted, which the URL learns a
  // moment later; reading it from here keeps the run's next turn on it.
  const [minted, setMinted] = useState("")
  const current = thread || minted

  const handleRef = useRef<ChatHandle | null>(null)
  const liveRef = useRef<LiveOverlay>(EMPTY_OVERLAY)
  const seqRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  // Every run gets a number, and a callback carrying a stale one is ignored:
  // aborting a stream does not un-queue what it already delivered.
  const runRef = useRef(0)
  // When the last run settled, or 0 when none is waiting to be handed over.
  // The handover is DERIVED from it against the query's own `dataUpdatedAt`.
  const [settledAt, setSettledAt] = useState(0)
  // The thread the in-flight run is writing to, once one is known: a new
  // chat's run that fails before its thread event has no rows to hand over to.
  const runThreadRef = useRef("")
  // The draft as of the last render, read by a failure that restores what
  // was sent only when nothing new has been typed since.
  const draftRef = useRef(draft)
  useEffect(() => {
    draftRef.current = draft
  }, [draft])

  // The stored transcript. Held still while the run streams: a mid-run read
  // returns rows the overlay is already showing, and the two would double.
  const messages = useQuery({
    ...threadMessagesQueryOptions(current),
    enabled: Boolean(current) && !streaming,
  })

  useEffect(() => () => handleRef.current?.stop(), [])

  const persisted = transcriptOrder(messages.data?.records ?? [])
  const handedOver = settledAt > 0 && messages.dataUpdatedAt >= settledAt
  // The read after a run failed for good: no handover will come on its own.
  // The overlay stays (it is the only view of the run), the composer is
  // released, and a retry that lands completes the handover.
  const reloadFailed =
    settledAt > 0 &&
    !handedOver &&
    messages.isError &&
    messages.errorUpdatedAt >= settledAt
  const turns = handedOver
    ? transcriptOf(persisted)
    : [...transcriptOf(persisted), ...live.turns]
  // A run that has settled but whose rows have not arrived is still busy: a
  // send in that window would push onto an overlay the refetch is about to
  // duplicate.
  const busy = streaming || (settledAt > 0 && !handedOver && !reloadFailed)
  const liveKey = streaming
    ? [...live.turns].reverse().find((t) => t.role === "assistant")?.key
    : undefined

  // The view follows the bottom of the conversation — as turns arrive and as
  // cards below them finish loading — until the reader scrolls up, and
  // picks it up again once they scroll back down.
  const stickRef = useRef(true)
  const contentRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = scrollRef.current
    const content = contentRef.current
    if (!el || !content || typeof ResizeObserver === "undefined") return
    const onScroll = () => {
      stickRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 48
    }
    const follow = new ResizeObserver(() => {
      if (stickRef.current) el.scrollTop = el.scrollHeight
    })
    el.addEventListener("scroll", onScroll, { passive: true })
    follow.observe(content)
    return () => {
      el.removeEventListener("scroll", onScroll)
      follow.disconnect()
    }
  }, [])

  function update(next: LiveOverlay) {
    liveRef.current = next
    setLive(next)
  }

  function onEvent(ev: AgentEvent, run: number) {
    if (run !== runRef.current) return
    switch (ev.kind) {
      case "thread":
        // A minted thread names itself on the first event; putting it in the
        // URL now keeps the address linkable even if the run is abandoned.
        if (ev.thread) runThreadRef.current = ev.thread
        if (ev.thread && ev.thread !== current) {
          setMinted(ev.thread)
          onThread(ev.thread)
        }
        break
      case "delta":
        if (ev.text)
          update(pushDelta(liveRef.current, ev.text, seqRef.current++))
        break
      case "toolStarted":
        update(
          pushToolStart(
            liveRef.current,
            {
              id: ev.id ?? "",
              name: ev.tool ?? "tool",
              arguments: ev.args ?? "",
            },
            seqRef.current++
          )
        )
        break
      case "toolFinished":
        update(
          settleTool(
            liveRef.current,
            {
              id: ev.id ?? "",
              name: ev.tool ?? "tool",
              arguments: ev.args ?? "",
            },
            ev.output ?? "",
            ev.ok ?? true,
            seqRef.current++
          )
        )
        break
      case "done":
        if (ev.result) setResult(ev.result)
        break
    }
  }

  function send(text = draft) {
    const message = text.trim()
    if (!message || busy || !agentId || keylessProvider) return
    onDraft("")
    setError(null)
    setResult(null)
    seqRef.current++
    // The previous run's overlay is the persisted transcript's job now —
    // unless its read failed, when the overlay is still the only view of it.
    setSettledAt(0)
    update({
      turns: [
        ...(reloadFailed ? liveRef.current.turns : []),
        {
          key: `live-u${seqRef.current}`,
          role: "user",
          content: message,
          tools: [],
        },
      ],
      closed: false,
    })
    setStreaming(true)
    runThreadRef.current = current
    const run = ++runRef.current

    // Both endings hand over the same way: the rows the loop wrote are the
    // truth, so they are marked stale and swapped in once they land. An error
    // ends a run too — leaving the overlay up beside a refetched transcript
    // would double every turn it had shown.
    const settle = () => {
      if (run !== runRef.current) return
      setSettledAt(Date.now())
      setStreaming(false)
      void client.invalidateQueries({ queryKey: ["records"] })
      // A turn's effects land as ROWS, so the surfaces counting them are
      // stale too. Coarse on purpose: the run reports a tally, not which
      // collections moved.
      void client.invalidateQueries({ queryKey: ["records-count"] })
    }

    // A run that never named a thread wrote nothing, so no read will ever
    // hand over and settling would leave the composer busy for good. The
    // optimistic turn goes, and the message comes back to be retried.
    const end = () => {
      if (run !== runRef.current) return
      if (runThreadRef.current) return settle()
      update(EMPTY_OVERLAY)
      setStreaming(false)
      if (!draftRef.current.trim()) onDraft(message)
    }

    handleRef.current = streamChat({
      agent: agentId,
      thread: current || undefined,
      message,
      onEvent: (ev) => onEvent(ev, run),
      onError: (err) => {
        if (run !== runRef.current) return
        setError(err.message)
        end()
      },
      onDone: end,
    })
  }

  const loading = messages.isPending && messages.isFetching
  const empty = turns.length === 0 && !streaming && !messages.isFetching
  const description =
    typeof agent?.properties.description === "string"
      ? agent.properties.description
      : ""
  const name = agentId ? agentName(agentId) : "an agent"

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex shrink-0 items-center gap-2.5 border-b px-4 py-3 md:px-6">
        {agentId && <AgentMark id={agentId} size="md" />}
        <div className="min-w-0 flex-1">
          <h1 className="truncate font-semibold">{title}</h1>
          <p className="flex flex-wrap items-center gap-x-2 text-[12.5px] text-muted-foreground">
            <span>with {name}</span>
            {technical && current && (
              <IdText value={`${LLM_PACKAGE}/thread/${current}`} copy />
            )}
            {technical && threadRecord && (
              <span className="text-faint">
                {tallyWords(threadTally(threadRecord)).join(" · ")}
              </span>
            )}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      </header>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <div
          ref={contentRef}
          className="flex max-w-[860px] flex-col gap-[18px] px-4 pt-6 pb-8 md:px-8"
        >
          {loading && (
            <p className="text-sm text-muted-foreground">
              Loading the messages…
            </p>
          )}
          {empty && current && (
            <p className="text-sm text-muted-foreground">
              This chat has no messages yet.
            </p>
          )}
          {empty && !current && agentId && (
            <div className="flex flex-col gap-3 py-6">
              <AgentMark id={agentId} size="lg" />
              <h2 className="text-xl font-semibold tracking-tight">{name}</h2>
              {description && (
                <p className="max-w-[560px] text-muted-foreground">
                  {description}
                </p>
              )}
              <div className="flex flex-wrap gap-2 pt-2">
                {examplePrompts(agent).map((prompt) => (
                  <button
                    key={prompt}
                    type="button"
                    onClick={() => onDraft(prompt)}
                    className="cursor-pointer rounded-lg border bg-background px-3 py-1.5 text-left text-[13px] text-muted-foreground hover:bg-hover hover:text-foreground"
                  >
                    {prompt}
                  </button>
                ))}
              </div>
            </div>
          )}
          {empty && !current && !agentId && (
            <p className="text-sm text-muted-foreground">
              There is no agent to chat with yet. Add a package that ships one
              from Providers.
            </p>
          )}
          <Transcript
            turns={turns}
            liveKey={liveKey}
            agentId={agentId}
            agent={agent}
          />
          {error && (
            <div className="rounded-lg border border-destructive/30 bg-bad-soft px-3.5 py-2.5 text-[13px] text-destructive">
              {name} couldn’t answer: {error}
            </div>
          )}
          {reloadFailed && (
            <div
              role="alert"
              className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-destructive/30 bg-bad-soft px-3.5 py-2.5 text-[13px] text-destructive"
            >
              <span>
                The conversation didn’t reload after that answer:{" "}
                {messages.error?.message}
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={messages.isFetching}
                onClick={() => void messages.refetch()}
              >
                Try again
              </Button>
            </div>
          )}
          {technical && result && (
            <p className="font-mono text-[11px] text-faint">
              {result.status}
              {result.reason ? ` (${result.reason})` : ""} · {result.turns}{" "}
              turns · {result.toolCalls} tool calls ·{" "}
              {result.totalTokens.toLocaleString()} tokens
              {result.costUSD ? ` · $${result.costUSD.toFixed(4)}` : ""}
            </p>
          )}
        </div>
      </div>

      <footer className="shrink-0 border-t">
        {keylessProvider && (
          <div className="px-4 pt-3 md:px-6">
            <KeylessCallout provider={keylessProvider} />
          </div>
        )}
        <Composer
          value={draft}
          onChange={onDraft}
          onSend={() => send()}
          agentId={agentId}
          agents={current ? undefined : pickable}
          onPickAgent={current ? undefined : onPickAgent}
          busy={busy}
          disabled={!agentId || Boolean(keylessProvider)}
        />
      </footer>
    </div>
  )
}
