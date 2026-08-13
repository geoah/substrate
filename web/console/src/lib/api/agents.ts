/** Agents: the agent registry rows, the llmprovider rows they complete
 * against, the llmthread/llmmessage transcript records, and the ndjson chat
 * stream. Agents/llmproviders/llmthreads/llmmessages are ordinary records the
 * generic browse renders; this module adds the agent-scoped reads and the one
 * thing the record API cannot do — the streaming chat loop.
 *
 * The whole agent-loop vocabulary lives in CORE: core absorbed the runtime
 * kinds, so `llmprovider`, `llmthread` and `llmmessage` sit beside `agent`
 * under `core.substrate.reamde.dev` — there is no separate runtime authority
 * to seed. */

import { queryOptions } from "@tanstack/react-query"

import { recordsQueryOptions } from "./records"
import { corePath, CORE_AUTHORITY, envelopeError } from "./http"
import { getToken, sessionExpired } from "./session"
import { ApiError, type SubstrateRecord } from "./types"

/** The agent rows — declared agents, one per `core.substrate.reamde.dev/agent` record. */
export function agentsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    plural: "agents",
    first: 200,
    orderBy: "createdAt:desc",
  })
}

/** The llmprovider rows — one endpoint each, addressed by an agent's
 * `provider` beside the plain `model` id it sends. */
export function providersQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    plural: "llmproviders",
    first: 200,
    orderBy: "createdAt:desc",
  })
}

/** Where a provider row sends its completions. `wire` is the protocol, not the
 * vendor, so an empty `baseURL` means that wire's own default endpoint — for
 * `openai` that is the host's configured gateway. `azure` has no such default:
 * a deployment IS its host, so the loop refuses a row without a baseURL and the
 * console must not read that row as if it would work. */
export function providerEndpoint(record: SubstrateRecord): string {
  const base = record.properties.baseURL
  if (typeof base === "string" && base) return base
  switch (record.properties.wire) {
    case "openai":
      return "host gateway"
    case "azure":
      return "missing baseURL"
    default:
      return "default endpoint"
  }
}

/** Whether a provider row carries its own key. A secret-typed property reads
 * back redacted, so set / not set is the only question the console can answer;
 * not set means the host's key, and only over the host's gateway. */
export function providerHasKey(record: SubstrateRecord): boolean {
  const key = record.properties.apiKey
  return typeof key === "string" && key !== ""
}

/** One agent's threads, newest first — its run history (a thread IS a run). */
export function agentThreadsQueryOptions(agent: string, first = 50) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    plural: "llmthreads",
    first,
    filter: { properties: { agent: { eq: agent } } },
    orderBy: "startedAt:desc",
  })
}

/** One thread's turns, in loop order — the transcript the chat surface
 * re-reads. Ordered by `turn` then created, so tool rows sit after the
 * assistant turn that dispatched them. */
export function threadMessagesQueryOptions(threadId: string) {
  return queryOptions({
    ...recordsQueryOptions({
      authority: CORE_AUTHORITY,
      plural: "llmmessages",
      first: 500,
      filter: { edge: { rel: "thread", to: threadId } },
      orderBy: "turn:asc,createdAt:asc",
    }),
    // A live chat re-reads the settled transcript once the stream ends.
    enabled: Boolean(threadId),
  })
}

// ── the chat stream ─────────────────────────────────────────────────────────

/** One settled agent invocation (substrate.AgentResult). */
export interface AgentResult {
  reply: string
  thread: string
  status: string
  reason?: string
  effects: number
  effectsByAction?: Record<string, number>
  turns: number
  toolCalls: number
  promptTokens: number
  completionTokens: number
  totalTokens: number
  costUSD: number
}

/** One streamed loop event (substrate.AgentEvent). */
export interface AgentEvent {
  kind: "thread" | "delta" | "toolStarted" | "toolFinished" | "done" | "error"
  /** Rides the first event: the thread id, minted or continued. */
  thread?: string
  /** A streamed content delta. */
  text?: string
  /** Tool lifecycle. `id` is the tool CALL's id and rides both sides: one turn
   * may dispatch the same tool twice, so a client pairing by name settles the
   * wrong card. It is the id the transcript's tool rows carry as
   * `toolCallId`, so a live card and its replayed row are the same card.
   * `output` rides the finished event: the dispatch's result payload. */
  id?: string
  tool?: string
  args?: string
  ok?: boolean
  output?: string
  /** Rides the done event. */
  result?: AgentResult
  /** Rides the error event: a post-200 loop failure. */
  error?: string
}

/** One ndjson line off the chat stream, or null (blank line / garbage). */
export function parseAgentEvent(line: string): AgentEvent | null {
  const trimmed = line.trim()
  if (!trimmed) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (typeof parsed !== "object" || parsed === null) return null
  const obj = parsed as Record<string, unknown>
  if (typeof obj.kind !== "string") return null
  return obj as unknown as AgentEvent
}

export interface ChatHandle {
  /** Aborts the in-flight stream. */
  stop(): void
}

/** Open or continue a thread against an agent with one user message and stream
 * the run: one AgentEvent per line (thread first, deltas and tool lifecycle as
 * they happen, one done event carrying the AgentResult). The fetch body never
 * ends until the loop settles, so `request` cannot carry it — this reads the
 * raw ndjson body. */
export function streamChat(opts: {
  agent: string
  /** Continues an existing thread; empty opens one. */
  thread?: string
  message: string
  onEvent: (event: AgentEvent) => void
  onError?: (error: Error) => void
  onDone?: () => void
}): ChatHandle {
  const ctrl = new AbortController()

  void (async () => {
    try {
      const headers: Record<string, string> = {
        Accept: "application/x-ndjson",
        "Content-Type": "application/json",
        "X-Substrate-Actor": "console",
      }
      const token = getToken()
      if (token) headers.Authorization = `Bearer ${token}`
      const res = await fetch(
        `${corePath("agents", opts.agent)}/chat`,
        {
          method: "POST",
          headers,
          body: JSON.stringify({
            thread: opts.thread ?? "",
            message: opts.message,
          }),
          signal: ctrl.signal,
        }
      )
      if (res.status === 401) {
        sessionExpired()
        opts.onError?.(new ApiError("auth", "session expired", 401))
        return
      }
      if (!res.ok || !res.body) throw envelopeError(res.status, undefined)

      // A post-200 loop failure arrives as an `error` event (or, from an older
      // server, a `done` with text and no result). Either routes to onError and
      // suppresses onDone, so the surface never settles a blank assistant turn
      // on top of an error.
      let errored = false
      const handle = (event: AgentEvent) => {
        const isError =
          event.kind === "error" ||
          (event.kind === "done" && !event.result && Boolean(event.error || event.text))
        if (isError) {
          errored = true
          opts.onError?.(
            new Error(event.error || event.text || "the agent run failed")
          )
          return
        }
        opts.onEvent(event)
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ""
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let nl = buffer.indexOf("\n")
        for (; nl >= 0; nl = buffer.indexOf("\n")) {
          const event = parseAgentEvent(buffer.slice(0, nl))
          buffer = buffer.slice(nl + 1)
          if (event) handle(event)
        }
      }
      const tail = parseAgentEvent(buffer)
      if (tail) handle(tail)
      if (!errored) opts.onDone?.()
    } catch (cause) {
      if (ctrl.signal.aborted) return
      const detail =
        cause instanceof ApiError
          ? cause.message
          : ((cause as Error).message ?? "chat stream failed")
      opts.onError?.(new Error(detail))
    }
  })()

  return { stop: () => ctrl.abort() }
}

/** The assistant reply a settled thread carries — the newest assistant
 * message's content, read back off the transcript. */
export function lastAssistantReply(messages: SubstrateRecord[]): string | undefined {
  for (let i = messages.length - 1; i >= 0; i--) {
    const p = messages[i].properties
    if (p.role === "assistant" && typeof p.content === "string" && p.content) {
      return p.content
    }
  }
  return undefined
}
