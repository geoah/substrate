/** Agent chat (`/agents/:id`, id = the agent identity): open or continue a
 * thread against the agent over the ndjson streaming endpoint. The thread
 * event names the thread, deltas stream the assistant turn, tool lifecycle
 * events show what the loop is doing, and the done event settles with the
 * run's tallies. The transcript persists as thread/message records either
 * way — this surface is only the live loop plus a link into that data. */

import { useEffect, useRef, useState } from "react"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, SendIcon, WrenchIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import {
  streamChat,
  type AgentEvent,
  type AgentResult,
  type ChatHandle,
} from "@/lib/api/agents"
import { agentChatRoute } from "@/router"
import { cn } from "@/lib/utils"

interface ToolActivity {
  tool: string
  args?: string
  ok?: boolean
}

interface Turn {
  role: "user" | "assistant"
  content: string
  tools?: ToolActivity[]
}

/** The route component is a thin shell: keying the surface on the agent id
 * remounts it fresh when navigating between agents, so no manual reset of the
 * conversation state is needed. */
export function AgentChatPage() {
  const { id } = agentChatRoute.useParams()
  return <ChatSurface key={id} id={id} />
}

function ChatSurface({ id }: { id: string }) {
  const [turns, setTurns] = useState<Turn[]>([])
  const [threadId, setThreadId] = useState("")
  const [live, setLive] = useState("")
  const [tools, setTools] = useState<ToolActivity[]>([])
  const [streaming, setStreaming] = useState(false)
  const [result, setResult] = useState<AgentResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [input, setInput] = useState("")

  const handleRef = useRef<ChatHandle | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  // Accumulators the done event reads without racing React's async state.
  const liveRef = useRef("")
  const toolsRef = useRef<ToolActivity[]>([])

  useEffect(() => () => handleRef.current?.stop(), [])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [turns, live, tools])

  function onEvent(ev: AgentEvent) {
    switch (ev.kind) {
      case "thread":
        if (ev.thread) setThreadId(ev.thread)
        break
      case "delta":
        if (ev.text) {
          liveRef.current += ev.text
          setLive(liveRef.current)
        }
        break
      case "toolStarted":
        toolsRef.current = [...toolsRef.current, { tool: ev.tool ?? "tool", args: ev.args }]
        setTools(toolsRef.current)
        break
      case "toolFinished": {
        const next = [...toolsRef.current]
        // settle the newest unsettled call of this tool name.
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i].tool === ev.tool && next[i].ok === undefined) {
            next[i] = { ...next[i], ok: ev.ok ?? true }
            break
          }
        }
        toolsRef.current = next
        setTools(next)
        break
      }
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
    // flush any prior live/tools into the record before the next turn.
    setTurns((prev) => [...prev, { role: "user", content: message }])
    liveRef.current = ""
    toolsRef.current = []
    setLive("")
    setTools([])
    setStreaming(true)

    handleRef.current = streamChat({
      agent: id,
      thread: threadId || undefined,
      message,
      onEvent,
      onError: (err) => {
        setError(err.message)
        setStreaming(false)
      },
      onDone: () => {
        setStreaming(false)
        // settle the assistant turn from whatever streamed.
        setTurns((prev) => [
          ...prev,
          {
            role: "assistant",
            content: liveRef.current,
            tools: toolsRef.current.length ? toolsRef.current : undefined,
          },
        ])
        liveRef.current = ""
        toolsRef.current = []
        setLive("")
        setTools([])
      },
    })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">{id}</h1>
          <p className="data text-xs text-muted-foreground">
            {threadId ? (
              <Link
                to="/data/$authority/$plural/$id"
                params={{
                  authority: "core.substrate.reamde.dev",
                  plural: "llmthreads",
                  id: threadId,
                }}
                className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
              >
                thread {threadId}
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

      <ScrollArea className="min-h-0 flex-1">
        <div ref={scrollRef} className="mx-auto flex max-w-3xl flex-col gap-3 px-6 pb-6">
          {turns.length === 0 && !streaming && (
            <p className="py-8 text-center text-sm text-muted-foreground">
              Send a message to open a thread against this agent.
            </p>
          )}
          {turns.map((turn, i) => (
            <MessageBubble key={i} turn={turn} />
          ))}
          {streaming && (
            <MessageBubble
              turn={{ role: "assistant", content: live, tools }}
              live
            />
          )}
          {error && (
            <div className="rounded-md border border-destructive/50 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}
          {result && (
            <div className="rounded-md border bg-muted/40 px-3 py-2 data text-xs text-muted-foreground">
              {result.status}
              {result.reason ? ` (${result.reason})` : ""}, {result.turns} turns,{" "}
              {result.toolCalls} tool calls, {result.totalTokens.toLocaleString()} tokens
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
            placeholder="Message the agent…  (⌘↵ to send)"
            disabled={streaming}
            className="min-h-0 resize-none"
          />
          <Button onClick={send} disabled={streaming || !input.trim()}>
            {streaming ? <Spinner className="size-3.5" /> : <SendIcon className="size-3.5" />}
            Send
          </Button>
        </div>
      </div>
    </div>
  )
}

function MessageBubble({ turn, live }: { turn: Turn; live?: boolean }) {
  const isUser = turn.role === "user"
  return (
    <div className={cn("flex flex-col gap-1", isUser ? "items-end" : "items-start")}>
      <span className="px-1 text-[0.65rem] uppercase tracking-wide text-muted-foreground">
        {turn.role}
      </span>
      {turn.tools && turn.tools.length > 0 && (
        <div className="flex flex-col gap-1">
          {turn.tools.map((t, i) => (
            <div key={i} className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <WrenchIcon className="size-3" />
              <span className="data">{t.tool}</span>
              {t.ok === undefined ? (
                <Spinner className="size-3" />
              ) : (
                <span className={cn("data", t.ok ? "text-muted-foreground" : "text-destructive")}>
                  {t.ok ? "ok" : "failed"}
                </span>
              )}
            </div>
          ))}
        </div>
      )}
      {(turn.content || (live && !turn.tools?.length)) && (
        <div
          className={cn(
            "max-w-[85%] rounded-md px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]",
            isUser ? "bg-primary text-primary-foreground" : "border bg-muted/40"
          )}
        >
          {turn.content}
          {live && <span className="ml-0.5 inline-block h-3.5 w-1 animate-pulse bg-current align-middle" />}
        </div>
      )}
    </div>
  )
}
