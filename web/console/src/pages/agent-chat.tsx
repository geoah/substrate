/** Agent chat (`/agents/:id`, id = the agent identity): the agent's threads on
 * the left, one thread's conversation on the right. A thread IS a run, and the
 * transcript IS records — `llmthread` + `llmmessage` rows the loop writes as it
 * goes — so this surface reads them back and nothing it showed while streaming
 * is lost on reload.
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
 * conversation blinks back to its pre-run state for a round trip. */

import { useEffect, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, SendIcon } from "lucide-react"
import { parseAsString, useQueryState } from "nuqs"

import { ThreadRail } from "@/components/agent/thread-rail"
import { Transcript } from "@/components/agent/transcript"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import {
  streamChat,
  threadMessagesQueryOptions,
  transcriptOrder,
  type AgentEvent,
  type AgentResult,
  type ChatHandle,
} from "@/lib/api/agents"
import {
  transcriptOf,
  type ToolCallView,
  type TurnView,
} from "@/lib/api/transcript"
import { agentChatRoute } from "@/router"

/** The live overlay. `closed` marks the assistant turn as finished with its
 * tools: the loop dispatches every call of a turn in sequence, so a finished
 * call does NOT end the turn — but the next prose delta does begin a new one. */
interface Live {
  turns: TurnView[]
  closed: boolean
}

const EMPTY: Live = { turns: [], closed: false }

/** Appends a delta, starting a new assistant turn when the previous one has
 * already dispatched and settled its tools. */
function pushDelta(live: Live, text: string, seq: number): Live {
  const turns = [...live.turns]
  const last = turns[turns.length - 1]
  if (!last || last.role !== "assistant" || live.closed) {
    turns.push({
      key: `live-a${seq}`,
      role: "assistant",
      content: text,
      tools: [],
    })
    return { turns, closed: false }
  }
  turns[turns.length - 1] = { ...last, content: last.content + text }
  return { turns, closed: false }
}

/** Attaches a started call to the assistant turn in flight, opening one when
 * the turn dispatched before it said anything. */
function pushToolStart(live: Live, call: ToolCallView, seq: number): Live {
  const turns = [...live.turns]
  const last = turns[turns.length - 1]
  if (!last || last.role !== "assistant") {
    turns.push({
      key: `live-a${seq}`,
      role: "assistant",
      content: "",
      tools: [call],
    })
    return { ...live, turns }
  }
  turns[turns.length - 1] = { ...last, tools: [...last.tools, call] }
  return { ...live, turns }
}

/** Settles a call BY ID: one turn may dispatch the same tool twice, and
 * settling by name would close the wrong card.
 *
 * A finish with no card is not a no-op. The loop refuses a dispatch past the
 * tool-call budget WITHOUT starting it — the refusal is a finished event and
 * nothing else — so the one card a reader most needs ("stop calling tools")
 * would never appear live, only on reload. An unclaimed finish opens its own
 * card, already settled. */
function settleTool(
  live: Live,
  call: ToolCallView,
  output: string,
  ok: boolean,
  seq: number
): Live {
  const settled = { ...call, output, ok }
  // An empty id cannot identify anything, so it settles the OLDEST card still
  // running rather than every id-less card at once.
  const match = (c: ToolCallView) =>
    call.id ? c.id === call.id : c.ok === undefined
  let claimed = false
  const turns = live.turns.map((turn) => {
    if (claimed || !turn.tools.some(match)) return turn
    claimed = true
    let done = false
    return {
      ...turn,
      tools: turn.tools.map((c) => {
        if (done || !match(c)) return c
        done = true
        return { ...c, output, ok }
      }),
    }
  })
  if (claimed) return { turns, closed: true }
  return {
    turns: [
      ...turns,
      { key: `live-a${seq}`, role: "assistant", content: "", tools: [settled] },
    ],
    closed: true,
  }
}

/** Keying the surface on the agent id remounts it fresh when navigating
 * between agents, so no manual reset of the conversation state is needed. */
export function AgentChatPage() {
  const { id } = agentChatRoute.useParams()
  return <ChatSurface key={id} id={id} />
}

function ChatSurface({ id }: { id: string }) {
  const client = useQueryClient()
  const [thread, setThread] = useQueryState(
    "thread",
    parseAsString.withDefault("").withOptions({ history: "push" })
  )
  const [live, setLive] = useState<Live>(EMPTY)
  const [streaming, setStreaming] = useState(false)
  const [result, setResult] = useState<AgentResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [input, setInput] = useState("")

  const handleRef = useRef<ChatHandle | null>(null)
  const liveRef = useRef<Live>(EMPTY)
  const seqRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  // Every run gets a number, and a callback carrying a stale one is ignored:
  // aborting a stream does not un-queue what it already delivered, so without
  // this an abandoned run can still write into the thread that replaced it.
  const runRef = useRef(0)
  // When the last run settled, or 0 when none is waiting to be handed over.
  // The handover is DERIVED from it against the query's own `dataUpdatedAt`:
  // an effect that cleared the overlay would be a setState inside a render
  // cascade, and there is nothing to store that the two timestamps do not
  // already say. Written only from callbacks, read only in render.
  const [settledAt, setSettledAt] = useState(0)

  // The stored transcript. Held still while the run streams: a mid-run read
  // returns rows the overlay is already showing, and the two would double.
  const messages = useQuery({
    ...threadMessagesQueryOptions(thread),
    enabled: Boolean(thread) && !streaming,
  })

  useEffect(() => () => handleRef.current?.stop(), [])

  // The wire hands back the NEWEST window first; the fold wants loop order.
  const persisted = transcriptOrder(messages.data?.records ?? [])
  // THE HANDOVER, derived: the transcript that landed after the run settled
  // contains everything the overlay was showing, so the overlay stops being
  // rendered the moment that read lands — and not one render earlier, which
  // would blank the conversation for a round trip.
  const handedOver = settledAt > 0 && messages.dataUpdatedAt >= settledAt
  const turns = handedOver
    ? transcriptOf(persisted)
    : [...transcriptOf(persisted), ...live.turns]
  // A run that has settled but whose rows have not arrived is still busy: a
  // send in that window would push onto an overlay the refetch is about to
  // duplicate.
  const busy = streaming || (settledAt > 0 && !handedOver)
  // The caret belongs to the turn still ARRIVING, which is only ever one the
  // overlay owns. Marking "the last assistant turn" would put it on the
  // previous run's settled answer between send and the first delta.
  const liveKey = streaming
    ? [...live.turns].reverse().find((t) => t.role === "assistant")?.key
    : undefined

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [turns.length, live])

  function update(next: Live) {
    liveRef.current = next
    setLive(next)
  }

  function onEvent(ev: AgentEvent, run: number) {
    if (run !== runRef.current) return
    switch (ev.kind) {
      case "thread":
        // A minted thread names itself on the first event; putting it in the
        // URL now keeps the address linkable even if the run is abandoned.
        if (ev.thread && ev.thread !== thread) void setThread(ev.thread)
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

  function send() {
    const message = input.trim()
    if (!message || busy) return
    setInput("")
    setError(null)
    setResult(null)
    seqRef.current++
    // The previous run's overlay is the persisted transcript's job now.
    setSettledAt(0)
    update({
      turns: [
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
    const run = ++runRef.current

    // Both endings hand over the same way: the rows the loop wrote are the
    // truth, so they are marked stale and the effect above swaps the overlay
    // out once they land. An error ends a run too — leaving the overlay up
    // beside a refetched transcript would double every turn it had shown.
    const settle = () => {
      if (run !== runRef.current) return
      setSettledAt(Date.now())
      setStreaming(false)
      // Marking stale is all this has to do: re-enabling the query is what
      // fetches, and `handedOver` above watches for the result.
      void client.invalidateQueries({ queryKey: ["records"] })
    }

    handleRef.current = streamChat({
      agent: id,
      thread: thread || undefined,
      message,
      onEvent: (ev) => onEvent(ev, run),
      onError: (err) => {
        if (run !== runRef.current) return
        setError(err.message)
        settle()
      },
      onDone: settle,
    })
  }

  /** Reading another thread abandons the live overlay: the run's rows are
   * already written, so the transcript is not lost — only this view of it. */
  function selectThread(next: string) {
    handleRef.current?.stop()
    // Past this the old run's callbacks are nobody's: they cannot write into
    // the thread being opened.
    runRef.current++
    setSettledAt(0)
    setStreaming(false)
    setResult(null)
    setError(null)
    update(EMPTY)
    void setThread(next || null)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">{id}</h1>
          <p className="data text-xs text-muted-foreground">
            {thread ? (
              <Link
                to="/data/$authority/$plural/$id"
                params={{
                  authority: "core.substrate.reamde.dev",
                  plural: "llmthreads",
                  id: thread,
                }}
                className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
              >
                thread {thread}
                <ArrowUpRightIcon className="size-3" />
              </Link>
            ) : (
              "new conversation"
            )}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          render={<Link to="/agents">All agents</Link>}
        />
      </div>

      <div className="flex min-h-0 flex-1 border-t">
        <ThreadRail
          agent={id}
          selected={thread}
          onSelect={selectThread}
          onNew={() => selectThread("")}
        />

        <div className="flex min-h-0 flex-1 flex-col">
          <ScrollArea className="min-h-0 flex-1">
            <div
              ref={scrollRef}
              className="mx-auto flex max-w-3xl flex-col gap-3 px-6 py-5"
            >
              {/* `isPending` is true for a DISABLED query too, so it cannot
                  mean "loading" here — only a fetch in flight can. */}
              {messages.isPending && messages.isFetching && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  Loading the transcript…
                </p>
              )}
              {turns.length === 0 && !streaming && !messages.isFetching && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  {thread
                    ? "This thread has no turns."
                    : "Send a message to open a thread against this agent."}
                </p>
              )}
              <Transcript turns={turns} liveKey={liveKey} />
              {error && (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                  {error}
                </div>
              )}
              {result && (
                <div className="rounded-md border bg-muted/40 px-3 py-2 data text-xs text-muted-foreground">
                  {result.status}
                  {result.reason ? ` (${result.reason})` : ""}, {result.turns}{" "}
                  turns, {result.toolCalls} tool calls,{" "}
                  {result.totalTokens.toLocaleString()} tokens
                  {result.costUSD ? `, $${result.costUSD.toFixed(4)}` : ""}
                </div>
              )}
            </div>
          </ScrollArea>

          <div className="shrink-0 border-t px-6 py-3">
            <div className="mx-auto flex max-w-3xl items-end gap-2">
              <Textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault()
                    send()
                  }
                }}
                rows={2}
                placeholder={
                  thread
                    ? "Continue this thread…  (⌘↵ to send)"
                    : "Message the agent…  (⌘↵ to send)"
                }
                disabled={busy}
                className="min-h-0 resize-none"
              />
              <Button onClick={send} disabled={busy || !input.trim()}>
                {streaming ? (
                  <Spinner className="size-3.5" />
                ) : (
                  <SendIcon className="size-3.5" />
                )}
                Send
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
