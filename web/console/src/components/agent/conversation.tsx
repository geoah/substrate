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
 * new chat's run mints its thread — remounting then would drop the stream.
 *
 * A turn runs on the server whether or not this page stays (decision 0169).
 * Opening a thread whose record says `running` attaches to it once the stored
 * rows are read: the replay's events the rows already hold are dropped by
 * their `turn`, and the rest feed the same overlay a send does. Stop ends
 * the turn on the server, and the stream's own `done` settles the overlay. */

import {
  useEffect,
  useEffectEvent,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { KeyRoundIcon, SquareIcon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { AddKeyButton } from "@/components/agent/model-key-dialog"
import { Composer } from "@/components/agent/composer"
import { ThreadHeading } from "@/components/agent/thread-heading"
import { Transcript } from "@/components/agent/transcript"
import { IdText } from "@/components/identity/id-text"
import { Button } from "@/components/ui/button"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useLiveRecords } from "@/hooks/use-live-records"
import {
  agentName,
  examplePrompts,
  isReplayed,
  persistedTurns,
  providerName,
  tallyWords,
  threadSubject,
  threadTally,
} from "@/lib/agent-chat"
import {
  attachThread,
  stopThread,
  streamChat,
  threadMessagesQueryOptions,
  transcriptOrder,
  type AgentEvent,
  type AgentResult,
  type ChatHandle,
} from "@/lib/api/agents"
import { LLM_PACKAGE } from "@/lib/api/http"
import {
  EMPTY_OVERLAY,
  pushCompacted,
  pushDelta,
  pushReasoning,
  pushToolStart,
  settleTool,
  transcriptOf,
  type LiveOverlay,
} from "@/lib/api/transcript"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"

/** The callout for an agent whose provider row has no key: the run would be
 * refused at dispatch, so the page says why before anybody types. */
function KeylessCallout({ provider }: { provider: string }) {
  return (
    <div
      role="note"
      className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-warning/30 bg-warn-soft px-3.5 py-2.5 text-[13px]"
    >
      <KeyRoundIcon className="size-4 shrink-0 text-warning" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="font-medium">
          This agent can’t answer yet: add an API key for{" "}
          {providerName(provider)}
        </span>
        <span className="text-muted-foreground">
          Agents run on your own key, kept sealed on the provider’s record.
        </span>
      </div>
      <AddKeyButton providerId={provider} />
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
  autoSend = false,
  onAutoSent,
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
  /** Send the draft as soon as the conversation opens, once: another page
   * handed the question over to be asked, not only written down. */
  autoSend?: boolean
  /** The hand-over was used (sent, or held for a missing key). */
  onAutoSent?: () => void
  /** The header's buttons. */
  actions?: ReactNode
}) {
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  const [live, setLive] = useState<LiveOverlay>(EMPTY_OVERLAY)
  const [streaming, setStreaming] = useState(false)
  const [result, setResult] = useState<AgentResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  // A turn no client can stream holds the thread (a triggered run): the page
  // says so and re-reads the thread as it changes, instead of waiting.
  const [background, setBackground] = useState(false)
  const [stopping, setStopping] = useState(false)
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

  // A re-attach waiting out its backoff.
  const retryRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  // The last failure an attach saw, shown if the turn turns out to have
  // ended on it.
  const lastErrorRef = useRef<string | null>(null)
  useEffect(
    () => () => {
      handleRef.current?.stop()
      clearTimeout(retryRef.current)
    },
    []
  )

  // While a turn runs elsewhere, every write to a thread re-reads the
  // records, so the thread's settle reaches this page.
  useLiveRecords(background ? [`${LLM_PACKAGE}/thread`] : [], [["records"]])

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
      case "reasoning":
        if (ev.text)
          update(pushReasoning(liveRef.current, ev.text, seqRef.current++))
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
              ...(ev.callable ? { callable: ev.callable } : {}),
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
              ...(ev.callable ? { callable: ev.callable } : {}),
              arguments: ev.args ?? "",
            },
            ev.output ?? "",
            ev.ok ?? true,
            seqRef.current++
          )
        )
        break
      case "compacted":
        update(
          pushCompacted(
            liveRef.current,
            { tokensBefore: ev.tokensBefore, covered: ev.covered },
            seqRef.current++
          )
        )
        break
      case "done":
        if (ev.result) setResult(ev.result)
        break
    }
  }

  // The hand-over sends once, the first time the conversation can.
  const autoSent = useRef(false)
  const handOver = useEffectEvent(() => {
    onAutoSent?.()
    send()
  })
  useEffect(() => {
    if (!autoSend || autoSent.current || !agentId) return
    autoSent.current = true
    handOver()
  }, [autoSend, agentId])

  // Both endings hand over the same way: the rows the loop wrote are the
  // truth, so they are marked stale and swapped in once they land. An error
  // ends a run too — leaving the overlay up beside a refetched transcript
  // would double every turn it had shown.
  function settleRun(run: number) {
    if (run !== runRef.current) return
    setSettledAt(Date.now())
    setStreaming(false)
    setStopping(false)
    void client.invalidateQueries({ queryKey: ["records"] })
    // A turn's effects land as ROWS, so the surfaces counting them are
    // stale too. Coarse on purpose: the run reports a tally, not which
    // collections moved.
    void client.invalidateQueries({ queryKey: ["records-count"] })
  }

  // The turn running on the thread, attached once per running stretch.
  // Each attach reads the stored rows afresh and freezes them, so the
  // replay's events those rows already hold are dropped by their `turn`.
  const threadRunning = threadRecord?.properties.status === "running"
  const [attachedFor, setAttachedFor] = useState("")
  // The attach to open, decided while rendering or by a stream that ended
  // without its `done`; the effect below opens it.
  const [pending, setPending] = useState<{
    thread: string
    attempt: number
  } | null>(null)
  if (!threadRunning && attachedFor) setAttachedFor("")
  if (!threadRunning && background) setBackground(false)
  if (current && threadRunning && !busy && attachedFor !== current) {
    setAttachedFor(current)
    setPending({ thread: current, attempt: 0 })
    setError(null)
    setResult(null)
    setSettledAt(0)
    setStreaming(true)
  }

  // A refusal the same request would meet again: a 4xx other than a
  // conflict or a rate limit, which can clear on their own.
  function permanent(err: Error): boolean {
    return (
      err instanceof ApiError &&
      err.status >= 400 &&
      err.status < 500 &&
      err.status !== 409 &&
      err.status !== 429
    )
  }

  // A stream that ended without `done` (a dropped connection, a stream
  // that fell behind, a server restart) is not the turn's end: attach again,
  // backing off, until the server says the turn settled.
  function reattach(run: number, thread: string, attempt: number) {
    if (run !== runRef.current) return
    clearTimeout(retryRef.current)
    const delay = attempt === 0 ? 0 : Math.min(1000 * 2 ** (attempt - 1), 15000)
    retryRef.current = setTimeout(() => {
      if (run === runRef.current) setPending({ thread, attempt })
    }, delay)
  }

  const openAttach = useEffectEvent(
    ({ thread, attempt }: { thread: string; attempt: number }) => {
      runThreadRef.current = thread
      const run = ++runRef.current
      void messages.refetch().then((read) => {
        if (run !== runRef.current) return
        if (read.isError) return reattach(run, thread, attempt + 1)
        const held = persistedTurns(read.data?.records ?? [])
        update(EMPTY_OVERLAY)
        let idle = ""
        let done = false
        handleRef.current = attachThread({
          thread,
          onEvent: (ev) => {
            if (run !== runRef.current) return
            if (ev.kind === "idle") {
              idle = ev.status || "unknown"
              if (ev.status === "running") setBackground(true)
              else if (lastErrorRef.current) setError(lastErrorRef.current)
              return
            }
            if (ev.kind === "done") {
              done = true
              lastErrorRef.current = null
            }
            if (isReplayed(ev, held)) return
            onEvent(ev, run)
          },
          onError: (err) => {
            if (run !== runRef.current) return
            if (permanent(err)) {
              // Attaching again would meet the same answer (a deleted
              // thread): say so and release the composer.
              setError(err.message)
              return settleRun(run)
            }
            lastErrorRef.current = err.message
            reattach(run, thread, attempt + 1)
          },
          onDone: () => {
            if (run !== runRef.current) return
            if (done) return settleRun(run)
            if (!idle) return reattach(run, thread, attempt + 1)
            if (idle === "running") {
              // A run no client can stream: nothing to show live.
              update(EMPTY_OVERLAY)
              setStreaming(false)
              setStopping(false)
              void client.invalidateQueries({ queryKey: ["records"] })
              return
            }
            settleRun(run)
          },
        })
      })
    }
  )
  useEffect(() => {
    if (pending) openAttach(pending)
  }, [pending])

  function stop() {
    const thread = runThreadRef.current || current
    if (!thread || stopping) return
    setStopping(true)
    stopThread(thread).catch((err: Error) => {
      setStopping(false)
      // The turn settled on its own first: its stream's `done` is the
      // answer, and there is nothing to say about the stop.
      if (err instanceof ApiError && err.status === 409) return
      setError(err.message)
    })
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
    // This page's own run is not attached to again.
    setAttachedFor(current)
    const run = ++runRef.current
    const settle = () => settleRun(run)

    // A run that never named a thread wrote nothing, so no read will ever
    // hand over and settling would leave the composer busy for good. The
    // optimistic turn goes, and the message comes back to be retried. A run
    // that named one and ended without `done` is still running on the
    // server: it is attached to again.
    // Whether this request's stream named its thread: before that, a failure
    // is a refusal (the repository's cap, a running turn, a spend cap) and
    // nothing ran, so the message comes back.
    let done = false
    let named = false
    const end = () => {
      if (run !== runRef.current) return
      if (named && done) return settle()
      if (named) return reattach(run, runThreadRef.current, 0)
      update(EMPTY_OVERLAY)
      setStreaming(false)
      setStopping(false)
      if (!draftRef.current.trim()) onDraft(message)
    }

    handleRef.current = streamChat({
      agent: agentId,
      thread: current || undefined,
      message,
      onEvent: (ev) => {
        if (ev.kind === "thread" && ev.thread && run === runRef.current) {
          named = true
          setAttachedFor(ev.thread)
        }
        if (ev.kind === "done") done = true
        onEvent(ev, run)
      },
      onError: (err) => {
        if (run !== runRef.current) return
        if (named) lastErrorRef.current = err.message
        else setError(err.message)
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
          <ThreadHeading
            title={title}
            subject={threadSubject(threadRecord)}
            thread={current}
          />
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
        <div className="flex shrink-0 items-center gap-1">
          {streaming && (
            <Button
              size="sm"
              variant="outline"
              onClick={stop}
              disabled={stopping}
            >
              <SquareIcon />
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          )}
          {actions}
        </div>
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
          {background && (
            <p role="status" className="text-sm text-muted-foreground">
              {name} is working on this chat in the background. Its answer shows
              here when it finishes.
            </p>
          )}
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
            <p className="text-[11.5px] text-faint tabular-nums">
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
          onStop={streaming ? stop : undefined}
          stopping={stopping}
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
