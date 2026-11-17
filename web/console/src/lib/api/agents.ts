/** Agents: the agent registry rows, the llm/provider rows they complete
 * against, the llm/thread/llm/message transcript records, and the ndjson chat
 * stream. The agent, llm/provider, llm/thread and llm/message rows are ordinary
 * records the generic browse renders; this module adds the agent-scoped
 * reads and the one thing the record API cannot do — the streaming chat loop.
 *
 * The agent row is core's (`substrate.reamde.dev/core/agent`); the runtime
 * rows it produces — provider, thread, message, interaction — are the seeded
 * `substrate.reamde.dev/llm` package's (decision record 0077). */

import { queryOptions } from "@tanstack/react-query"

import {
  createRecord,
  fetchRecordsPage,
  patchRecord,
  recordsQueryOptions,
  type FirstPageParams,
} from "./records"
import { deleteRecord } from "./sync"
import {
  collectionPath,
  corePath,
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  LLM_PACKAGE,
  LLM_PACKAGE_NAME,
  envelopeError,
  request,
} from "./http"
import { walkPages } from "./compacted"
import { getToken, sessionExpired } from "./session"
import {
  ApiError,
  REFERENCE_KEY,
  type Page,
  type ServerErrorCode,
  type SubstrateRecord,
} from "./types"
import { AGENT_BUDGET_KIND, AGENT_KIND, LLM_MODEL_KIND } from "@/lib/core-kinds"

/** The agent rows — declared agents, one per `substrate.reamde.dev/core/agent` record. */
export function agentsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: "agent",
    first: 200,
    orderBy: "createdAt:desc",
  })
}

/** Where a provider row sends its completions. `wire` is the protocol, not the
 * vendor, and only `anthropic` has an endpoint of its own: there is no host
 * gateway, so an `openai` or `azure` row without a `baseURL` refuses at
 * dispatch and the console must not read it as if it would work. */
export function providerEndpoint(record: SubstrateRecord): string {
  const base = record.properties.baseURL
  if (typeof base === "string" && base) return base
  switch (record.properties.wire) {
    case "openai":
    case "azure":
      return "missing baseURL"
    default:
      return "default endpoint"
  }
}

/** Whether a provider row carries its own key. A secret-typed property reads
 * back redacted, so set / not set is the only question the console can answer.
 * A row runs on its own key, or on the operator's where the server supplies
 * one for that row id (`GET /api/v1/operator/defaults` says which). */
export function providerHasKey(record: SubstrateRecord): boolean {
  const key = record.properties.apiKey
  return typeof key === "string" && key !== ""
}

/** The modes a person reads as a conversation: every run of a root agent.
 * A sub-agent's thread and a judge's verdict are parts of another run, and
 * they stay under Data. */
export const CONVERSATION_MODES = [
  "chat",
  "manual",
  "call",
  "record",
  "schedule",
  "webhook",
]

/** How many conversations the chat list reads. */
export const CHAT_LIST_WINDOW = 100

/** The key every list of threads starts with: the chat list, the newest
 * chat, an agent's runs. A partial key, so invalidating it reaches each of
 * them whatever its filter and window, and no transcript, which lists
 * messages. */
export const THREAD_LISTS_KEY = ["records", [`${LLM_PACKAGE}/thread`]] as const

/** Every agent's conversations, most recently active first: the chat list.
 * Ordered by `updatedAt`, which every settle moves, so a continued chat
 * rises to the top. */
export function conversationsQueryOptions(first = CHAT_LIST_WINDOW) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "thread",
    first,
    filter: { properties: { mode: { in: CONVERSATION_MODES } } },
    orderBy: "updatedAt:desc",
  })
}

/** The newest chat a person had, with whichever agent. Its own read, not the
 * chat list's first row: that list mixes in every trigger's and schedule's
 * runs, and a hundred of them would push the last chat out of its window. */
export function lastChatQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "thread",
    first: 1,
    filter: { properties: { mode: { eq: "chat" } } },
    orderBy: "startedAt:desc",
  })
}

/** How many of one agent's runs its page reads: the wire's page cap
 * (`substrate.Query.First`), so every figure on the page comes from one
 * read and the page says where that read stopped. */
export const AGENT_RUNS_WINDOW = 500

/** One agent's runs, most recently active first: every thread it ran,
 * whatever started it (a chat, a trigger, another agent, a judgement).
 * Ordered by `updatedAt`, which every settle moves, so an old chat that went
 * on today is inside the read, and a read that reaches back past a moment
 * holds every run active since it. */
export function agentRunsQueryOptions(agentId: string) {
  return queryOptions({
    ...recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: LLM_PACKAGE_NAME,
      name: "thread",
      first: AGENT_RUNS_WINDOW,
      filter: {
        properties: {
          agent: {
            eq: `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/agent/${agentId}`,
          },
        },
      },
      orderBy: "updatedAt:desc",
    }),
    enabled: Boolean(agentId),
  })
}

/** Every agent's runs, most recently active first, in one read: what the
 * repository's spend cap counts. Ordered by `updatedAt` for the reason
 * `agentRunsQueryOptions` is, so a figure over the last 24 hours knows whether
 * the read reached back that far. `deleted` reads the tombstoned runs
 * instead: the engine counts a deleted run's cost, because the money was
 * spent, and a list reads live rows or tombstones, never both. A tombstone's
 * `updatedAt` is when it was deleted, after it finished, so the same bound
 * holds for it. */
export function repositoryRunsQueryOptions({ deleted = false } = {}) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "thread",
    first: AGENT_RUNS_WINDOW,
    orderBy: "updatedAt:desc",
    filter: deleted ? { deleted: true } : undefined,
  })
}

/** The user messages of a set of threads, oldest turn first, so the first
 * one per thread is what opened it (`openingMessages` reads it). One read for
 * the whole list: `thread` is a reference whose declaration pins the kind, so
 * bare ids are admitted. */
export function openingMessagesQueryOptions(threadIds: string[]) {
  return queryOptions({
    ...recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: LLM_PACKAGE_NAME,
      name: "message",
      first: TRANSCRIPT_WINDOW,
      filter: {
        properties: { role: { eq: "user" }, thread: { in: threadIds } },
      },
      orderBy: "turn:asc,createdAt:asc",
    }),
    enabled: threadIds.length > 0,
  })
}

/** The llm/provider rows an agent completes against. */
export function llmProvidersQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "provider",
    first: 100,
  })
}

/** Whether the server answered that it does not know the llm/model kind. The
 * records read refuses an undeclared filter kind as a validation error naming
 * it (422, code `validation`, `unknown type "<kind>"`), which is what a
 * server older than the kind answers; any other refusal is a real one. */
export function modelKindUnknown(error: unknown): boolean {
  return kindUnknown(error, LLM_MODEL_KIND)
}

/** Whether a read was refused because the server does not know the kind. */
function kindUnknown(error: unknown, kind: string): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.code === "validation" &&
    error.message.includes(`unknown type "${kind}"`)
  )
}

/** The llm/model records: what each provider row offers and prices, and
 * what an agent's `runsOn` names (decision record 0242). Every page is read:
 * a record past the first would read as missing and move a runsOn choice the
 * engine does not move. `enabled` holds the read until a surface needs it. A
 * server older than the kind holds no model records, and its rows price from
 * their own entries alone, so its refusal reads as no records. */
export function llmModelsQueryOptions(enabled = true) {
  const params: FirstPageParams = {
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "model",
    first: 500,
  }
  return queryOptions({
    ...recordsQueryOptions(params),
    queryFn: async ({ signal }): Promise<Page> => {
      try {
        const { pages } = await walkPages(
          `${CORE_AUTHORITY}/${LLM_PACKAGE_NAME}/model`,
          (after: string | undefined) =>
            fetchRecordsPage({ ...params, after }, signal),
          (page) => page.cursor || undefined
        )
        return {
          records: pages.flatMap((page) => page.records),
          head: pages[0].head,
          generation: pages[0].generation,
        }
      } catch (error) {
        if (modelKindUnknown(error))
          return { records: [], head: 0, generation: "" }
        throw error
      }
    },
    enabled,
  })
}

/** The owner's overrides of one agent's budgets (`llm/agentbudget`,
 * decision record 0314). The server keeps one live record per agent, so this
 * is one record or none, but an import folds history in without that check,
 * so every record naming the agent is read, every page of them, and the
 * caller picks the one the engine holds to (heldBudget): one left unread
 * could be it, and a save or a removal would leave it standing. A server
 * older than the kind refuses it as unknown, which reads as none. */
export function agentBudgetsQueryOptions(agentId: string) {
  const params: FirstPageParams = {
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "agentbudget",
    first: AGENT_BUDGETS_READ,
    filter: { properties: { agent: { eq: `${AGENT_KIND}/${agentId}` } } },
  }
  return queryOptions({
    queryKey: [...recordsQueryOptions(params).queryKey, "agentbudget"],
    queryFn: async ({ signal }): Promise<SubstrateRecord[]> => {
      try {
        const { pages } = await walkPages(
          AGENT_BUDGET_KIND,
          (after: string | undefined) =>
            fetchRecordsPage({ ...params, after }, signal),
          (page) => page.cursor || undefined
        )
        return pages.flatMap((page) => page.records)
      } catch (error) {
        if (kindUnknown(error, AGENT_BUDGET_KIND)) return []
        throw error
      }
    },
  })
}

/** The page size of the overrides read: one override is the rule, so the
 * first page is the whole list everywhere but after an odd import. */
const AGENT_BUDGETS_READ = 50

/** Set the owner's daily cap for one agent, in US cents, on the override
 * that holds, or on a new one where there is none. The others an import left
 * are deleted first: the server refuses a write to one override while
 * another names the same agent. */
export async function saveAgentSpendCap(
  agentId: string,
  cents: number,
  held: SubstrateRecord | undefined,
  others: readonly SubstrateRecord[]
): Promise<void> {
  for (const record of others) await deleteRecord(record)
  if (held) {
    await patchRecord(
      CORE_AUTHORITY,
      LLM_PACKAGE_NAME,
      "agentbudget",
      held.id,
      { properties: { spendCentsPerDay: cents } }
    )
    return
  }
  await createRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "agentbudget", {
    properties: {
      agent: { [REFERENCE_KEY]: `${AGENT_KIND}/${agentId}` },
      spendCentsPerDay: cents,
    },
  })
}

/** Remove every override of one agent's budgets, so the cap it declares
 * holds again. The daily cap is the only limit one carries today. */
export async function removeAgentBudgets(
  budgets: readonly SubstrateRecord[]
): Promise<void> {
  for (const record of budgets) await deleteRecord(record)
}

/** The standing rules for agent writes (`recordpatchpolicy`). */
export function writePoliciesQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: "recordpatchpolicy",
    first: 200,
  })
}

/** The policies that speak for one agent's writes: enabled, and naming the
 * agent's identity or no agent at all (an empty list matches every agent). */
export function policiesForAgent(
  agentId: string,
  policies: SubstrateRecord[]
): SubstrateRecord[] {
  return policies.filter((policy) => {
    if (policy.properties.disabled === true) return false
    const selector = policy.properties.selector
    const agents =
      selector && typeof selector === "object" && !Array.isArray(selector)
        ? (selector as Record<string, unknown>).agents
        : undefined
    if (!Array.isArray(agents) || agents.length === 0) return true
    return agents.includes(agentId)
  })
}

/** One thread's turns, in loop order — the transcript the chat surface
 * re-reads. Ordered by `turn` then created, so tool rows sit after the
 * assistant turn that dispatched them. */
export function threadMessagesQueryOptions(threadId: string) {
  return queryOptions({
    ...recordsQueryOptions(threadMessagesParams(threadId)),
    // A live chat re-reads the settled transcript once the stream ends.
    enabled: Boolean(threadId),
    // NOT the list default: `placeholderData: (prev) => prev` would hold the
    // PREVIOUS thread's transcript on screen under the next thread's id — and
    // hold it forever on a new conversation, where the query never runs.
    placeholderData: undefined,
  })
}

function threadMessagesParams(threadId: string): FirstPageParams {
  return {
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "message",
    first: TRANSCRIPT_WINDOW,
    // `thread` is a REFERENCE property on the message.
    filter: { properties: { thread: { eq: threadId } } },
    // DESCENDING, then reversed by the reader: the page cap is a cap, and a
    // long conversation must lose its oldest turns rather than the reply
    // that just streamed. `turn` is int-typed, so this is a numeric sort.
    orderBy: "turn:desc,createdAt:desc",
  }
}

/** How many turns of one thread the surface reads. The wire caps a page at
 * 500, and nothing here pages further: past this a conversation shows its most
 * recent window. */
export const TRANSCRIPT_WINDOW = 500

/** The transcript in loop order, from the newest-first page the wire returns. */
export function transcriptOrder(records: SubstrateRecord[]): SubstrateRecord[] {
  return [...records].reverse()
}

// ── the chat stream ─────────────────────────────────────────────────────────

/** What ended a run that did not finish: which failure ended an `error`
 * run, which budget an `overbudget` one, what stopped a `stopped` one
 * (substrate.AgentCauses, decision 0315). The set is closed and
 * `agentcauses.golden.test.ts` holds this union to the server's. A thread
 * carries it as `cause`; the console's words for each are in
 * `lib/turn-outcome.ts` and nowhere else. */
export type TurnCause =
  | "allowance_spent"
  | "context_too_long"
  | "deadline"
  | "internal"
  | "interrupted"
  | "model_unavailable"
  | "provider_billing"
  | "provider_key_refused"
  | "provider_overloaded"
  | "provider_rate_limited"
  | "provider_rejected"
  | "provider_unavailable"
  | "requested"
  | "shutdown"
  | "tool_call_limit"
  | "turn_limit"
  | "write_failed"

/** One settled agent invocation (substrate.AgentResult). */
export interface AgentResult {
  reply: string
  thread: string
  status: string
  reason?: string
  /** What ended a run a budget or a stop ended; absent on an `ok` run. A
   * string and not `TurnCause`: a newer server may send one this build does
   * not know, which reads as its `status` alone. */
  cause?: string
  /** Whole seconds the provider asked to wait, beside a cause that names a
   * wait. */
  retryAfter?: number
  effects: number
  effectsByAction?: Record<string, number>
  turns: number
  toolCalls: number
  promptTokens: number
  completionTokens: number
  totalTokens: number
  costUSD: number
  /** The records the final reply links, as its row stores them. */
  mentions?: AgentMention[]
}

/** One record a reply links (substrate.AgentMention), read by the engine
 * when the run settled. */
export interface AgentMention {
  kind: string
  id: string
  /** The link's label as the model wrote it. */
  text: string
  /** No live record has this kind and id: the model named one that does not
   * exist, so the reply shows the label alone. */
  missing?: boolean
  /** Nothing the model saw in the run carried this record. */
  unread?: boolean
}

/** One streamed loop event (substrate.AgentEvent). */
export interface AgentEvent {
  kind:
    | "thread"
    | "delta"
    | "reasoning"
    | "toolStarted"
    | "toolFinished"
    | "compacted"
    | "truncated"
    | "idle"
    | "done"
    | "error"
    | "lagged"
    | "messageQueued"
    | "messageTaken"
  /** Rides the first event: the thread id, minted or continued. */
  thread?: string
  /** A streamed content delta, or on a `reasoning` event a piece of the
   * model's thinking, which arrives ahead of the turn's text. On a
   * `messageQueued` event, what the message says. */
  text?: string
  /** Tool lifecycle. `id` is the tool CALL's id and rides both sides: one turn
   * may dispatch the same tool twice, so a client pairing by name settles the
   * wrong card. It is the id the transcript's tool rows carry as
   * `toolCallId`, so a live card and its replayed row are the same card.
   * `callable` rides both sides too: the identity behind the `tool` alias, as
   * the rows stamp it. `output` rides the finished event: the dispatch's
   * result payload. On `messageQueued` and `messageTaken` (decision 0318)
   * `id` is the id of a message sent while the turn ran: its row is written,
   * then the model is handed it. */
  id?: string
  tool?: string
  callable?: string
  args?: string
  ok?: boolean
  output?: string
  /** Ride the compacted event: the loop folded older turns into a `summary`
   * message row. `tokensBefore` is the context size that triggered it and
   * `covered` the number of message rows the summary stands in for. */
  tokensBefore?: number
  covered?: number
  /** Rides the done event. */
  result?: AgentResult
  /** Rides the error event: a post-200 loop failure. */
  error?: string
  /** Rides an error event the stream itself ends with: the error's code,
   * `auth` where the stream's token no longer stands. */
  code?: ServerErrorCode
  /** Ride the error event of a turn that failed: the cause its thread
   * settled with, and the seconds the provider asked to wait. An error event
   * with no cause is the stream's own end, not the turn's. */
  cause?: string
  retryAfter?: number
  /** Rides delta, reasoning, toolStarted, toolFinished, messageQueued and
   * messageTaken: the `turn` ordinal the matching `llm/message` row carries
   * or will carry. An attach after reading the rows drops a replayed piece
   * of text and a replayed tool event whose ordinal a row it holds carries
   * (`isReplayed`). */
  turn?: number
  /** Rides delta and reasoning (decision 0317): how many Unicode code points
   * of this turn's text of that kind came before this piece. Text and
   * reasoning count apart, each from 0 with its `turn`, so a piece is
   * written where it belongs and a replay is safe to apply over what is
   * shown. Code points, not UTF-16 units: `Array.from(text).length`. */
  at?: number
  /** Rides the idle event: the thread row's status. */
  status?: string
  /** Rides a toolFinished event of a background call: `working` on the one
   * its handle answers, then where the job ended on a second toolFinished
   * under the same `id`, sent while a turn streams on the thread. */
  job?: "working" | "completed" | "failed" | "canceled"
  /** Ride the tool events (`agents` revision 8), RFC 3339 by the server's
   * clock: when the call's own dispatch started and, on toolFinished, ended,
   * as the tool row's `ran` says. On a background job's second toolFinished
   * they are the job's. */
  startedAt?: string
  finishedAt?: string
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
  // The counts are read as numbers or not at all: a line that spells one
  // some other way must not reach a sentence that formats it.
  for (const key of ["tokensBefore", "covered", "turn", "retryAfter", "at"]) {
    if (key in obj && typeof obj[key] !== "number") delete obj[key]
  }
  return obj as unknown as AgentEvent
}

export interface ChatHandle {
  /** Aborts the in-flight stream. The turn itself runs on: stopping it is
   * `stopThread`. */
  stop(): void
}

interface StreamHandlers {
  onEvent: (event: AgentEvent) => void
  onError?: (error: Error) => void
  onDone?: () => void
}

/** The path of a verb on one `llm/thread` record. */
function threadVerbPath(thread: string, verb: string): string {
  return `${collectionPath(CORE_AUTHORITY, LLM_PACKAGE_NAME, "thread")}/${encodeURIComponent(thread)}/${verb}`
}

/** The turn failed after its stream opened: the server's words, with the
 * cause its thread settled with. */
export class TurnFailedError extends Error {
  turnCause: string
  retryAfter?: number

  constructor(message: string, turnCause: string, retryAfter?: number) {
    super(message)
    this.name = "TurnFailedError"
    this.turnCause = turnCause
    this.retryAfter = retryAfter
  }
}

/** The connection failed, before or after the server answered: whether the
 * request took effect is unknown, unlike an `ApiError`, which is the
 * server's own answer. */
export class TransportError extends Error {}

/** Fetches an ndjson agent stream and reads it to its end. The body never
 * ends until the turn settles, so `request` cannot carry it. */
function openAgentStream(
  path: string,
  init: { method: "GET" | "POST"; body?: unknown },
  handlers: StreamHandlers
): ChatHandle {
  const ctrl = new AbortController()

  void (async () => {
    // Whether the server's answer began to arrive. A fetch that rejects
    // before it may still have delivered the request.
    let answered = false
    try {
      const headers: Record<string, string> = {
        Accept: "application/x-ndjson",
      }
      if (init.body !== undefined) headers["Content-Type"] = "application/json"
      const token = getToken()
      if (token) headers.Authorization = `Bearer ${token}`
      const res = await fetch(path, {
        method: init.method,
        headers,
        body: init.body === undefined ? undefined : JSON.stringify(init.body),
        signal: ctrl.signal,
      })
      answered = true
      if (res.status === 401) {
        sessionExpired()
        handlers.onError?.(new ApiError("auth", "session expired", 401))
        return
      }
      if (!res.ok) {
        // A refusal before the run started (an agent at its spend cap, a
        // running turn) is an error envelope, and its message says why.
        let body: unknown
        try {
          body = await res.json()
        } catch {
          body = undefined
        }
        // Without the server's envelope the answer is not the server's: a
        // gateway's 502, 503 or 504 can come after the server admitted the
        // turn, so it says nothing about whether the request took effect.
        const envelope = (body as { error?: { message?: unknown } } | undefined)
          ?.error
        if (typeof envelope?.message !== "string") {
          throw new TransportError(
            `the request ended with HTTP ${res.status} and no answer from the server`
          )
        }
        throw envelopeError(res.status, body)
      }
      if (!res.body) {
        throw new TransportError("the server answered without a stream")
      }
      await readAgentStream(res.body, handlers)
    } catch (cause) {
      if (ctrl.signal.aborted) return
      // An ApiError keeps its status, so a caller can tell a refusal that
      // will not change (a deleted thread) from a failure worth retrying.
      const message = (cause as Error).message || "chat stream failed"
      handlers.onError?.(
        cause instanceof ApiError || cause instanceof TransportError
          ? cause
          : new TransportError(
              answered
                ? `the connection closed while the answer was read (${message})`
                : `the connection failed before the server answered (${message})`
            )
      )
    }
  })()

  return { stop: () => ctrl.abort() }
}

/** Reads an agent stream's ndjson body to its end. A post-200 loop failure
 * arrives as an `error` event (or, from an older server, a `done` with text
 * and no result). Either routes to onError and suppresses onDone, so the
 * surface never settles a blank assistant turn on top of an error. */
export async function readAgentStream(
  body: ReadableStream<Uint8Array>,
  handlers: StreamHandlers
): Promise<void> {
  let errored = false
  const handle = (event: AgentEvent) => {
    const isError =
      event.kind === "error" ||
      (event.kind === "done" &&
        !event.result &&
        Boolean(event.error || event.text))
    if (isError) {
      errored = true
      // The stream's token no longer stands (revoked, or the person signed
      // out everywhere): the session is over, as on a 401.
      if (event.kind === "error" && event.code === "auth") {
        sessionExpired()
        handlers.onError?.(new ApiError("auth", "session expired", 401))
        return
      }
      const words = event.error || event.text || "the agent run failed"
      handlers.onError?.(
        event.kind === "error" && event.cause
          ? new TurnFailedError(words, event.cause, event.retryAfter)
          : new Error(words)
      )
      return
    }
    handlers.onEvent(event)
  }

  const reader = body.getReader()
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
  if (!errored) handlers.onDone?.()
}

/** One chat turn as the client sends it (substrate.ChatInput): the message,
 * the thread it continues, and the two fields the server renders into the
 * turn context after the message (decision 0196). */
export interface ChatRequest {
  /** Continues an existing thread; empty or absent opens one. */
  thread?: string
  message: string
  /** The id this client chose for the message (decision 0312): the server
   * writes the user row under it, and a request that repeats it writes
   * nothing and runs nothing. */
  messageId?: string
  /** The pending question (`llm/interaction`) of `thread` this message
   * answers in the person's own words (decision 0327): the server resolves
   * it with the message, and the turn that reads the message is the only
   * one that follows. It needs `thread` and `messageId`. */
  repliesTo?: string
  /** The client's IANA zone; absent falls back to the repository's
   * `substrate.reamde.dev/llm/timezone` setting, then UTC. */
  timezone?: string
  /** The record paths (`<kind>/<id>`) the message is about, at most
   * eight, each read under the agent's grant. */
  context?: string[]
}

/** The browser's IANA zone, or undefined where it names none. */
export function browserTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

/** The revision of the server's `agents` feature that takes a chat body's
 * `messageId` and serves a thread's `continue` (decision 0312). A console
 * bundled in an app meets older servers, which refuse the field as unknown
 * and answer the route 404. */
export const OWN_MESSAGE_ID_REVISION = 3

/** The revision of the server's `agents` feature whose streams say where
 * each piece of text belongs (`at`) and whose resumed chat runs stream
 * (decision 0317). Below it a piece is appended, so an attach starts its
 * overlay over. */
export const STREAM_POSITIONS_REVISION = 5

/** The revision of the server's `agents` feature that takes a message sent
 * to a thread a chat turn runs on (decision 0318): the message joins that
 * turn, and the stream says when it is queued and when the model is handed
 * it. Below it the same send is refused as busy. */
export const MESSAGE_DURING_RUN_REVISION = 6

/** The revision of the server's `agents` feature that takes a chat body's
 * `repliesTo`, a question answered with the message that names it, and
 * whose resolved questions, approvals and change requests link whose hand
 * resolved them (decision 0327). Below it the server refuses the field as
 * unknown, and a filter on the link as an undeclared property. */
export const REPLY_IN_WORDS_REVISION = 7

/** An id for a message this client is about to send: a UUID, which is a
 * legal record id. The fallback is for a page served without a secure
 * context, where `randomUUID` is absent. */
export function newMessageId(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

/** Open or continue a thread against an agent with one user message and stream
 * the run: one AgentEvent per line (thread first, deltas and tool lifecycle as
 * they happen, one done event carrying the AgentResult). The turn runs on the
 * server whether or not this stream stays open; `attachThread` picks it up
 * again.
 *
 * The message goes under `messageId`, so the same call made again is safe: a
 * message the server already holds writes nothing and runs nothing, and the
 * stream is the `thread` event and then what an attach answers (the running
 * turn, or one `idle` line).
 *
 * Sent to a thread a chat turn runs on, with a `messageId`, the message
 * joins that turn (`MESSAGE_DURING_RUN_REVISION`): the stream is again the
 * `thread` event and then that turn's. */
export function streamChat(
  opts: {
    agent: string
    /** Continues an existing thread; empty opens one. */
    thread?: string
    message: string
    /** The id this client chose for the message (`newMessageId`). Left out
     * for a server that does not take one (`OWN_MESSAGE_ID_REVISION`): it
     * refuses a body field it does not know. */
    messageId?: string
    /** The records the message is about, as record paths. */
    context?: string[]
    /** The pending question of `thread` the message answers
     * (`REPLY_IN_WORDS_REVISION`); sent only with a `messageId`. */
    repliesTo?: string
  } & StreamHandlers
): ChatHandle {
  const body: ChatRequest = {
    thread: opts.thread ?? "",
    message: opts.message,
    timezone: browserTimezone(),
  }
  if (opts.messageId) body.messageId = opts.messageId
  if (opts.repliesTo && opts.messageId) body.repliesTo = opts.repliesTo
  if (opts.context?.length) body.context = opts.context
  return openAgentStream(
    `${corePath("agent", opts.agent)}/chat`,
    { method: "POST", body },
    opts
  )
}

/** Attach to the chat turn running on a thread: what it emitted so far, then
 * live events, ending with `done`. With nothing running the stream is one
 * `idle` event carrying the thread's status. */
export function attachThread(
  opts: { thread: string } & StreamHandlers
): ChatHandle {
  return openAgentStream(
    threadVerbPath(opts.thread, "events"),
    { method: "GET" },
    opts
  )
}

/** Go on with a thread whose last turn stopped, ran out of budget or failed,
 * with no new message: the stored rows are the whole conversation, and the
 * stream is a chat turn's. A thread a turn runs on is refused with the
 * reason `thread_busy`, and one whose last turn finished with
 * `nothing_to_continue`. */
export function continueThread(
  opts: { thread: string } & StreamHandlers
): ChatHandle {
  return openAgentStream(
    threadVerbPath(opts.thread, "continue"),
    { method: "POST" },
    opts
  )
}

/** Stop the chat turn running on a thread. Resolves with the settled result
 * once the turn has settled (`status: "stopped"` unless it finished first);
 * any attached stream ends with the same `done`. */
export function stopThread(thread: string): Promise<AgentResult> {
  return request<AgentResult>("POST", threadVerbPath(thread, "stop"))
}

/** The assistant reply a settled thread carries — the newest assistant
 * message's content, read back off the transcript. */
export function lastAssistantReply(
  messages: SubstrateRecord[]
): string | undefined {
  for (let i = messages.length - 1; i >= 0; i--) {
    const p = messages[i].properties
    if (p.role === "assistant" && typeof p.content === "string" && p.content) {
      return p.content
    }
  }
  return undefined
}
