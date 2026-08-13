/** Agent chat (`/agents/:id`, id = the agent identity): the agent's threads on
 * the left, one thread's conversation on the right. A thread IS a run, and the
 * transcript IS records — `llmthread` + `llmmessage` rows the loop writes as it
 * goes — so this surface reads them back and nothing it showed while streaming
 * is lost on reload.
 *
 * TWO SOURCES, ONE RENDER PATH. The persisted rows are the truth; the live
 * ndjson stream is an overlay on top of them, folded into the same `TurnView`
 * shape (lib/api/transcript.ts). The overlay exists because the records query
 * is deliberately NOT refetched mid-run — a partial read would double every
 * turn the stream is already showing. When the run settles, the refetch is
 * awaited BEFORE the overlay is dropped, so the handover never blinks. */

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
  type AgentEvent,
  type AgentResult,
  type ChatHandle,
} from "@/lib/api/agents"
import { transcriptOf, type ToolCallView, type TurnView } from "@/lib/api/transcript"
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
    turns.push({ key: `live-a${seq}`, role: "assistant", content: text, tools: [] })
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
    turns.push({ key: `live-a${seq}`, role: "assistant", content: "", tools: [call] })
    return { ...live, turns }
  }
  turns[turns.length - 1] = { ...last, tools: [...last.tools, call] }
  return { ...live, turns }
}

/** Settles a call BY ID: one turn may dispatch the same tool twice, and
 * settling by name would close the wrong card. */
function settleTool(live: Live, id: string, output: string, ok: boolean): Live {
  const turns = live.turns.map((turn) => {
    if (!turn.tools.some((c) => c.id === id)) return turn
    return {
      ...turn,
      tools: turn.tools.map((c) => (c.id === id ? { ...c, output, ok } : c)),
    }
  })
  return { turns, closed: true }
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

  // The stored transcript. Held still while the run streams: a mid-run read
  // returns rows the overlay is already showing, and the two would double.
  const messages = useQuery({
    ...threadMessagesQueryOptions(thread),
    enabled: Boolean(thread) && !streaming,
  })

  useEffect(() => () => handleRef.current?.stop(), [])

  const persisted = messages.data?.records ?? []
  const turns = [...transcriptOf(persisted), ...live.turns]

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [turns.length, live])

  function update(next: Live) {
    liveRef.current = next
    setLive(next)
  }

  function onEvent(ev: AgentEvent) {
    switch (ev.kind) {
      case "thread":
        // A minted thread names itself on the first event; putting it in the
        // URL now keeps the address linkable even if the run is abandoned.
        if (ev.thread && ev.thread !== thread) void setThread(ev.thread)
        break
      case "delta":
        if (ev.text) update(pushDelta(liveRef.current, ev.text, seqRef.current++))
        break
      case "toolStarted":
        update(
          pushToolStart(
            liveRef.current,
            { id: ev.id ?? "", name: ev.tool ?? "tool", arguments: ev.args ?? "" },
            seqRef.current++
          )
        )
        break
      case "toolFinished":
        update(
          settleTool(liveRef.current, ev.id ?? "", ev.output ?? "", ev.ok ?? true)
        )
        break
      case "done":
        if (ev.result) setResult(ev.result)
        break
    }
  }

  function send() {
    const message = input.trim()
    if (!message || streaming) return
    setInput("")
    setError(null)
    setResult(null)
    seqRef.current++
    update({
      turns: [
        ...liveRef.current.turns,
        { key: `live-u${seqRef.current}`, role: "user", content: message, tools: [] },
      ],
      closed: false,
    })
    setStreaming(true)

    handleRef.current = streamChat({
      agent: id,
      thread: thread || undefined,
      message,
      onEvent,
      onError: (err) => {
        setError(err.message)
        setStreaming(false)
      },
      onDone: () => {
        setStreaming(false)
        // The refetch lands BEFORE the overlay is dropped: clearing first
        // would blank the conversation until the rows arrived.
        void (async () => {
          await client.invalidateQueries({ queryKey: ["records"] })
          update(EMPTY)
        })()
      },
    })
  }

  /** Reading another thread abandons the live overlay: the run's rows are
   * already written, so the transcript is not lost — only this view of it. */
  function selectThread(next: string) {
    handleRef.current?.stop()
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
              {messages.isPending && thread && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  Loading the transcript…
                </p>
              )}
              {turns.length === 0 && !streaming && !messages.isPending && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  {thread
                    ? "This thread has no turns."
                    : "Send a message to open a thread against this agent."}
                </p>
              )}
              <Transcript turns={turns} streaming={streaming} />
              {error && (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                  {error}
                </div>
              )}
              {result && (
                <div className="rounded-md border bg-muted/40 px-3 py-2 data text-xs text-muted-foreground">
                  {result.status}
                  {result.reason ? ` (${result.reason})` : ""}, {result.turns} turns,{" "}
                  {result.toolCalls} tool calls,{" "}
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
                disabled={streaming}
                className="min-h-0 resize-none"
              />
              <Button onClick={send} disabled={streaming || !input.trim()}>
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
