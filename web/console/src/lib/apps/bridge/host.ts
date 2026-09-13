/** The console's end of the bridge: one host per mounted guest. It holds the
 * port, answers `ui/initialize` with the host context, serves `tools/call`
 * for records, functions and agents with the console's OWN credential, keeps
 * each subscription as an observer on the console's query cache and pushes
 * the page whenever the tail moves it, relays the chrome (title, primary
 * action, toast, confirm, navigate, errors), and pings the guest so a
 * document that stopped answering is torn down rather than left drawing
 * stale data.
 *
 * Every read and every write is checked against the app's grant by RESOLVED
 * kind identity: the name the guest sends is looked up in the registry and
 * the grant is asked about what was found, so a spelling that is not a kind
 * is refused as unknown rather than matched as a string. A subscription is
 * held to the grant when it opens and again on every push. And the grant
 * counts only when the component says so (`granted`): below owner
 * provenance the guest gets no data and no calls, whatever the row's
 * `permissions` say, because an agent that may write apps could have
 * widened them under an owner-written `source`. */

import type { QueryClient } from "@tanstack/react-query"

import { streamChat, type AgentEvent, type ChatHandle } from "@/lib/api/agents"
import { collectionPath, corePath, request, seg } from "@/lib/api/http"
import {
  createRecord,
  deleteRecord,
  listPath,
  patchRecord,
  putRecord,
  type ListParams,
  type RecordPatch,
  type RecordWrite,
} from "@/lib/api/records"
import {
  ApiError,
  type FunctionCalled,
  type KindInfo,
  type Page,
  type SubstrateRecord,
} from "@/lib/api/types"
import { declarationOf } from "@/apps-sdk/declaration"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { mergePages, readsFor, runImplements, type Read } from "../fanout"
import {
  expandGrant,
  implementsTrait,
  type ExpandedGrant,
  type Grant,
} from "../grant"
import {
  createSubscriptions,
  type SubscriptionResult,
  type SubscriptionStore,
} from "../subscriptions"
import {
  Endpoint,
  ERROR,
  METHODS,
  PROTOCOL_VERSION,
  RpcError,
  TOOLS,
  type AgentEventPush,
  type BackResult,
  type CallToolParams,
  type CallToolResult,
  type ConfirmParams,
  type ConfirmResult,
  type GuestError,
  type HostContext,
  type InitializeResult,
  type ListArguments,
  type ListResult,
  type NavigateParams,
  type PrimaryActionParams,
  type RecordsPush,
  type RouteChangedParams,
  type SizeChangedParams,
  type SubscribeResult,
  type TitleParams,
  type ToastParams,
  type ToolFailure,
} from "./protocol"

/** How often the guest is asked whether it is still there, and how many
 * unanswered asks end the app. */
export const PING_MS = 5_000
export const PING_MISSES = 3

const HOST_INFO = { name: "substrate-console", version: "0" }

/** How long a back tap waits for the guest before the host navigates. */
export const BACK_MS = 300

/** The schemes `ui/open-link` may open. Anything else is refused before the
 * person is asked. */
const LINK_SCHEMES = new Set(["http:", "https:", "mailto:", "tel:"])

/** The properties whose provenance decides whether the grant is honored:
 * the code and the grant itself. */
export const GATED = ["source", "modules", "permissions"] as const

/** Whether every gated property the row carries was written at owner tier,
 * read off the single-record read's `propertyMeta` (lists omit it). A row
 * without the meta, written before the tiers, is below owner, not above it. */
export function ownerProvenance(record: SubstrateRecord | undefined): boolean {
  if (!record || typeof record.properties.source !== "string") return false
  const meta = record.propertyMeta ?? {}
  return GATED.every(
    (p) => record.properties[p] === undefined || meta[p]?.tier === "owner"
  )
}

export type Access =
  | { ok: true; kind: KindInfo }
  | {
      ok: false
      message: string
      /** The grant refusal as a problem: the property and the edit. */
      problem?: { path: string; message: string }
    }

export type Op = "read" | "write"

/** The one gate every records call goes through. `grant` is the expanded
 * grant (traits already implementors), `kinds` the registry. */
export function checkAccess(
  grant: ExpandedGrant,
  kinds: KindInfo[],
  op: Op,
  kind: unknown
): Access {
  if (typeof kind !== "string" || !kind) {
    return { ok: false, message: "the call names no kind" }
  }
  const resolved = kindByIdentity(kinds, kind)
  if (!resolved) return { ok: false, message: `unknown kind ${kind}` }
  const allowed = (op === "read" ? grant.reads : grant.writes).includes(
    resolved.identity
  )
  if (!allowed) {
    const path =
      op === "read" ? "permissions.reads.kinds" : "permissions.writes"
    return {
      ok: false,
      message: `the app's grant does not ${op} ${resolved.identity}; ${path}: add ${resolved.identity}`,
      problem: { path, message: `add ${resolved.identity}` },
    }
  }
  return { ok: true, kind: resolved }
}

/** The records calls the host makes on the guest's behalf, injectable so
 * the bridge is tested without a network. */
export interface RecordsApi {
  list(params: ListParams): Promise<Page>
  get(kind: KindInfo, id: string): Promise<SubstrateRecord>
  create(
    kind: KindInfo,
    input: RecordWrite,
    idempotencyKey?: string
  ): Promise<SubstrateRecord>
  put(kind: KindInfo, id: string, input: RecordWrite): Promise<SubstrateRecord>
  patch(
    kind: KindInfo,
    id: string,
    patch: RecordPatch
  ): Promise<SubstrateRecord>
  delete(kind: KindInfo, id: string, ifVersion?: number): Promise<void>
}

export interface FunctionsApi {
  /** The function's record: its `effect` and `confirmation` decide whether
   * the person is asked first. */
  get(identity: string): Promise<SubstrateRecord>
  call(
    identity: string,
    input: Record<string, unknown>,
    idempotencyKey: string
  ): Promise<FunctionCalled>
}

export interface AgentsApi {
  chat(opts: {
    agent: string
    thread?: string
    message: string
    onEvent: (event: AgentEvent) => void
    onError?: (error: Error) => void
    onDone?: () => void
  }): ChatHandle
}

function parts(kind: KindInfo) {
  const { authority, pkg, name } = splitKind(kind.identity)
  return { authority, pkg, name }
}

export const liveRecordsApi: RecordsApi = {
  list: (params) => request<Page>("GET", listPath(params)),
  get: (kind, id) => {
    const { authority, pkg, name } = parts(kind)
    return request<SubstrateRecord>(
      "GET",
      `${collectionPath(authority, pkg, name)}/${seg(id)}`
    )
  },
  create: (kind, input, idempotencyKey) => {
    const { authority, pkg, name } = parts(kind)
    return createRecord(authority, pkg, name, input, { idempotencyKey })
  },
  put: (kind, id, input) => {
    const { authority, pkg, name } = parts(kind)
    return putRecord(authority, pkg, name, id, input)
  },
  patch: (kind, id, patch) => {
    const { authority, pkg, name } = parts(kind)
    return patchRecord(authority, pkg, name, id, patch)
  },
  delete: (kind, id, ifVersion) => {
    const { authority, pkg, name } = parts(kind)
    return deleteRecord(authority, pkg, name, id, ifVersion)
  },
}

export const liveFunctionsApi: FunctionsApi = {
  get: (identity) =>
    request<SubstrateRecord>("GET", corePath("function", identity)),
  call: (identity, input, idempotencyKey) =>
    request<FunctionCalled>(
      "POST",
      `${corePath("function", identity)}/call`,
      { input },
      { idempotencyKey }
    ),
}

export const liveAgentsApi: AgentsApi = {
  chat: (opts) => streamChat(opts),
}

export interface HostCallbacks {
  hostContext: () => HostContext
  onInitialized?: () => void
  onSizeChanged?: (size: SizeChangedParams) => void
  onNavigate?: (target: NavigateParams) => void | Promise<void>
  onPrimaryAction?: (state: PrimaryActionParams | null) => void
  onTitle?: (text: string) => void
  onToast?: (toast: ToastParams) => void
  /** Ask the person: a guest's `confirm`, and the gate before a function
   * whose effect is external or irreversible. The default is the browser's
   * own confirm. */
  confirm?: (args: ConfirmParams) => boolean | Promise<boolean>
  /** Ask the person before a link opens; the default is the browser's own
   * confirm. */
  confirmLink?: (url: URL) => boolean | Promise<boolean>
  openLink?: (url: URL) => void
  /** A transform, runtime or grant problem the strip above the frame draws. */
  onError?: (error: GuestError) => void
  /** The kinds the open subscriptions read changed; the frame lifts them
   * into `useLiveRecords`. */
  onSubscribedKinds?: (kinds: string[]) => void
  /** The guest missed too many pings; the host has already torn down. */
  onLost?: () => void
}

/** What the host needs of the app record: the grant, and the id for
 * messages. `lib/apps/spec.ts`'s `AppSpec` satisfies it. */
export interface AppGrant {
  id: string
  permissions: Grant
}

export interface BridgeHostOptions {
  /** Read on every call, so an app saved while mounted is checked against
   * its new grant and not the one it mounted with; `kinds` is the registry. */
  app: () => { spec: AppGrant; kinds: KindInfo[] }
  /** Whether the grant is the owner's (see `ownerProvenance`). */
  granted: () => boolean
  callbacks: HostCallbacks
  /** The console's query client; subscriptions are observers on it. Without
   * one (or a `subscriptions` store) `subscribe` is refused. */
  queryClient?: QueryClient
  subscriptions?: SubscriptionStore
  api?: RecordsApi
  functions?: FunctionsApi
  agents?: AgentsApi
  pingMs?: number
}

export interface BridgeHost {
  attach(port: MessagePort): void
  hostContextChanged(partial: Partial<HostContext>): void
  primaryActionClicked(): void
  /** The host's back button was tapped: asked of the guest, which answers
   * whether a listener took it; false (or no answer within a beat) means
   * the host navigates itself. */
  back(): Promise<boolean>
  /** The app's own path moved (the host pushed history, or the person did). */
  routeChanged(path: string): void
  /** `ui/resource-teardown`, then the port closes. Idempotent. */
  teardown(): void
  /** The kinds the open subscriptions read. */
  subscribedKinds(): string[]
  readonly initialized: boolean
  readonly closed: boolean
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
}

function strList(v: unknown): string[] | undefined {
  return Array.isArray(v) && v.every((x) => typeof x === "string" && x)
    ? (v as string[])
    : undefined
}

function obj(v: unknown): Record<string, unknown> | undefined {
  return v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : undefined
}

function num(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined
}

function invalid(message: string): never {
  throw new RpcError(ERROR.invalidParams, message)
}

function ok(structuredContent: Record<string, unknown>): CallToolResult {
  return { content: [], structuredContent }
}

/** The API's envelope as the guest sees it, `code` and all, so a 422 reaches
 * a form as field problems and a 409 reaches a check as a conflict. */
export function failureOf(err: unknown): ToolFailure {
  if (err instanceof ApiError) {
    return {
      code: err.code,
      message: err.message,
      problems: err.problems.length ? err.problems : undefined,
      problemDetails: err.problemDetails.length
        ? err.problemDetails
        : undefined,
    }
  }
  return {
    code: "internal",
    message: err instanceof Error ? err.message : String(err),
  }
}

function toolError(err: unknown): CallToolResult {
  const failure = failureOf(err)
  return {
    isError: true,
    content: [{ type: "text", text: failure.message }],
    structuredContent: failure as unknown as Record<string, unknown>,
  }
}

const CONFIRMED_EFFECTS = new Set(["external", "irreversible"])

export function createBridgeHost(opts: BridgeHostOptions): BridgeHost {
  const api = opts.api ?? liveRecordsApi
  const functions = opts.functions ?? liveFunctionsApi
  const agents = opts.agents ?? liveAgentsApi
  const subs =
    opts.subscriptions ??
    (opts.queryClient ? createSubscriptions(opts.queryClient) : undefined)
  const pingMs = opts.pingMs ?? PING_MS
  const cb = opts.callbacks
  let rpc: Endpoint | undefined
  let initialized = false
  let closed = false
  let pinger: ReturnType<typeof setInterval> | undefined
  let misses = 0
  let awaitingPong = false
  let nextSubscription = 0
  let nextStream = 0
  const streams = new Map<string, ChatHandle>()

  let memo:
    { spec: AppGrant; kinds: KindInfo[]; grant: ExpandedGrant } | undefined
  const current = () => {
    const { spec, kinds } = opts.app()
    if (!memo || memo.spec !== spec || memo.kinds !== kinds) {
      memo = { spec, kinds, grant: expandGrant(spec, kinds) }
    }
    return memo
  }

  const refuse = (
    message: string,
    problem?: { path: string; message: string }
  ): never => {
    if (problem) cb.onError?.({ phase: "grant", ...problem })
    throw new RpcError(ERROR.forbidden, message)
  }

  const requireGranted = () => {
    if (!opts.granted()) {
      throw new RpcError(
        ERROR.forbidden,
        "the app's grant has not been written by the owner"
      )
    }
  }

  const grantFor = (op: Op, kind: unknown): KindInfo => {
    requireGranted()
    const { grant, kinds } = current()
    const access = checkAccess(grant, kinds, op, kind)
    if (!access.ok) return refuse(access.message, access.problem)
    return access.kind
  }

  /** A function or an agent is granted by its identity, which IS its record
   * id; the record read that follows is what says whether it exists. */
  const grantCallable = (op: "call" | "agents", ref: string): void => {
    requireGranted()
    const { grant } = current()
    if (!grant[op].includes(ref)) {
      const path = `permissions.${op}`
      refuse(
        `the app's grant does not ${op === "call" ? "call" : "chat with"} ${ref}; ${path}: add ${ref}`,
        { path, message: `add ${ref}` }
      )
    }
  }

  /** The kinds a read names, each held to the grant: one `kind`, several
   * `kinds`, or the implementors of a trait intersected with the grant. */
  const readKinds = (q: ListArguments): KindInfo[] => {
    if (q.kind) return [grantFor("read", q.kind)]
    if (q.kinds?.length) return q.kinds.map((k) => grantFor("read", k))
    if (q.implements) {
      requireGranted()
      const { grant } = current()
      const found = grant.readKinds.filter((k) =>
        implementsTrait(k, q.implements!)
      )
      if (!found.length) {
        return refuse(
          `the app's grant covers no kind implementing ${q.implements}; permissions.reads.traits: add ${q.implements}`,
          { path: "permissions.reads.traits", message: `add ${q.implements}` }
        )
      }
      return found
    }
    return invalid("list names a kind, kinds or a trait")
  }

  const listArgs = (args: Record<string, unknown>): ListArguments => ({
    kind: str(args.kind),
    kinds: strList(args.kinds),
    implements: str(args.implements),
    filter: obj(args.filter),
    orderBy: str(args.orderBy),
    first: num(args.first),
    after: str(args.after),
  })

  const stopPinging = () => {
    if (pinger !== undefined) clearInterval(pinger)
    pinger = undefined
  }

  const close = () => {
    if (closed) return
    closed = true
    stopPinging()
    subs?.closeAll()
    for (const handle of streams.values()) handle.stop()
    streams.clear()
    rpc?.close()
  }

  const lost = () => {
    close()
    cb.onLost?.()
  }

  const startPinging = () => {
    stopPinging()
    misses = 0
    awaitingPong = false
    pinger = setInterval(() => {
      if (!rpc || closed) return
      if (awaitingPong) misses++
      if (misses >= PING_MISSES) {
        lost()
        return
      }
      awaitingPong = true
      rpc.call(METHODS.ping).then(
        () => {
          misses = 0
          awaitingPong = false
        },
        () => {}
      )
    }, pingMs)
  }

  const confirm = (args: ConfirmParams) =>
    (cb.confirm ?? ((a: ConfirmParams) => window.confirm(a.title)))(args)

  const callTool = async (params: unknown): Promise<CallToolResult> => {
    const p = obj(params) as Partial<CallToolParams> | undefined
    const name = str(p?.name) ?? invalid("tools/call names a tool")
    const args = obj(p?.arguments) ?? {}
    switch (name) {
      case TOOLS.list: {
        const q = listArgs(args)
        const kinds = readKinds(q)
        try {
          // One kind is a fan-out of one: the same path, no merged cursor
          // lost, and the rewrite onto the kind's point is the identity.
          const result: ListResult = await runImplements(q, kinds, api.list)
          return ok(result as unknown as Record<string, unknown>)
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.get: {
        const kind = grantFor("read", args.kind)
        const id = str(args.id) ?? invalid("get names an id")
        try {
          return ok(
            (await api.get(kind, id)) as unknown as Record<string, unknown>
          )
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.put: {
        const kind = grantFor("write", args.kind)
        const properties =
          obj(args.properties) ?? invalid("put carries properties")
        const input: RecordWrite = {
          properties,
          labels: obj(args.labels),
          ifVersion: num(args.ifVersion),
        }
        const id = str(args.id)
        try {
          const record = id
            ? await api.put(kind, id, input)
            : await api.create(
                kind,
                input,
                str(args.idempotencyKey) ?? crypto.randomUUID()
              )
          return ok(record as unknown as Record<string, unknown>)
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.patch: {
        const kind = grantFor("write", args.kind)
        const id = str(args.id) ?? invalid("patch names an id")
        const properties =
          obj(args.properties) ?? invalid("patch carries properties")
        try {
          const record = await api.patch(kind, id, {
            properties,
            labels: obj(args.labels),
            ifVersion: num(args.ifVersion),
          })
          return ok(record as unknown as Record<string, unknown>)
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.delete: {
        const kind = grantFor("write", args.kind)
        const id = str(args.id) ?? invalid("delete names an id")
        try {
          await api.delete(kind, id, num(args.ifVersion))
          return ok({ deleted: true })
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.transition: {
        // A move is a read of the row and then a write of it; both arms of
        // the grant apply.
        const kind = grantFor("write", args.kind)
        grantFor("read", args.kind)
        const id = str(args.id) ?? invalid("transition names an id")
        const to = str(args.to) ?? invalid("transition names a state")
        const declaration = declarationOf(kind)
        const state = declaration.stateProperty(str(args.property))
        if (!state) {
          return toolError(
            new Error(
              str(args.property)
                ? `${kind.identity} has no state property ${String(args.property)}`
                : `${kind.identity} has no single state property; name one`
            )
          )
        }
        try {
          const row = await api.get(kind, id)
          const from = str(row.properties[state.name])
          if (!declaration.admits(state.name, from, to)) {
            return toolError(
              new Error(
                `${state.name} does not move from ${from ?? "unset"} to ${to}`
              )
            )
          }
          const record = await api.patch(kind, id, {
            properties: { [state.name]: to },
            ifVersion: num(args.ifVersion) ?? row.version,
          })
          return ok(record as unknown as Record<string, unknown>)
        } catch (err) {
          return toolError(err)
        }
      }
      case TOOLS.functionCall: {
        const ref = str(args.ref) ?? invalid("call names a function")
        grantCallable("call", ref)
        const input = obj(args.args) ?? {}
        try {
          const fn = await functions.get(ref)
          const effect = str(fn.properties.effect)
          const gated =
            fn.properties.confirmation === "always" ||
            (effect !== undefined && CONFIRMED_EFFECTS.has(effect))
          if (gated) {
            const title = str(fn.properties.name) ?? ref
            const agreed = await confirm({
              title: `Run ${title}?`,
              body: str(fn.properties.description),
              destructive: effect === "irreversible",
            })
            if (!agreed) {
              throw new RpcError(ERROR.forbidden, "the person declined")
            }
          }
          const called = await functions.call(
            ref,
            input,
            str(args.idempotencyKey) ?? crypto.randomUUID()
          )
          return ok({ output: called.output, effects: called.effects })
        } catch (err) {
          if (err instanceof RpcError) throw err
          return toolError(err)
        }
      }
      case TOOLS.agentChat: {
        const ref = str(args.ref) ?? invalid("chat names an agent")
        grantCallable("agents", ref)
        const message = str(args.message) ?? invalid("chat carries a message")
        const stream = `stream-${++nextStream}`
        const push = (event: AgentEvent) =>
          rpc?.notify(METHODS.agentEvent, {
            stream,
            event,
          } satisfies AgentEventPush)
        const handle = agents.chat({
          agent: ref,
          thread: str(args.thread),
          message,
          onEvent: push,
          onError: (error) => {
            push({ kind: "error", error: error.message })
            streams.delete(stream)
          },
          onDone: () => streams.delete(stream),
        })
        streams.set(stream, handle)
        return ok({ stream })
      }
      case TOOLS.agentStop: {
        const stream = str(args.stream) ?? invalid("stop names a stream")
        streams.get(stream)?.stop()
        streams.delete(stream)
        return ok({ stopped: true })
      }
      default:
        throw new RpcError(ERROR.methodNotFound, `unknown tool ${name}`)
    }
  }

  /** A declined confirm is the person's answer, not the app's failure: it
   * resolves `{opened: false}` rather than rejecting, so an app that fires
   * and forgets `host.openLink` does not put the refusal in the error strip. */
  const openLink = async (params: unknown): Promise<{ opened: boolean }> => {
    const raw = str(obj(params)?.url) ?? invalid("open-link carries a url")
    let url: URL
    try {
      url = new URL(raw)
    } catch {
      return invalid(`not a URL: ${raw}`)
    }
    if (!LINK_SCHEMES.has(url.protocol)) {
      throw new RpcError(ERROR.forbidden, `links may not open ${url.protocol}`)
    }
    const ask =
      cb.confirmLink ?? ((u: URL) => window.confirm(`Open ${u.href}?`))
    if (!(await ask(url))) {
      return { opened: false }
    }
    const open =
      cb.openLink ??
      ((u: URL) => {
        window.open(u.href, "_blank", "noopener,noreferrer")
      })
    open(url)
    return { opened: true }
  }

  /** The page a subscription's result becomes, held to the grant as it
   * stands now: an app saved with a narrower grant loses the data it no
   * longer covers on the next beat. */
  const pageOf = (
    q: ListArguments,
    reads: Read[],
    result: SubscriptionResult
  ): RecordsPush["page"] => {
    try {
      const allowed = new Set(readKinds(q).map((k) => k.identity))
      for (const r of reads) {
        if (!allowed.has(r.kind.identity)) {
          throw new RpcError(
            ERROR.forbidden,
            `the app's grant no longer reads ${r.kind.identity}`
          )
        }
      }
    } catch (err) {
      return {
        records: [],
        error: {
          code: "forbidden",
          message: err instanceof Error ? err.message : String(err),
        },
      }
    }
    const page = mergePages(reads, result.pages, q.orderBy)
    const failed = result.errors.find((e) => e)
    return failed ? { ...page, error: failureOf(failed) } : page
  }

  const subscribe = (params: unknown): Promise<SubscribeResult> => {
    if (!subs) {
      throw new RpcError(ERROR.internal, "this host holds no subscriptions")
    }
    const q = listArgs(obj(params) ?? {})
    const kinds = readKinds(q)
    const reads = readsFor(q, kinds)
    const id = `sub-${++nextSubscription}`
    return new Promise<SubscribeResult>((resolve) => {
      let answered = false
      subs.open(
        id,
        reads.map((r) => r.params),
        (result) => {
          if (!answered) {
            if (!result.settled) return
            answered = true
            resolve({ subscription: id, page: pageOf(q, reads, result) })
            return
          }
          if (!rpc || closed) return
          rpc.notify(METHODS.records, {
            subscription: id,
            page: pageOf(q, reads, result),
          } satisfies RecordsPush)
        }
      )
      cb.onSubscribedKinds?.(subs.kinds())
    })
  }

  const unsubscribe = (params: unknown): Record<string, never> => {
    const id =
      str(obj(params)?.subscription) ??
      invalid("unsubscribe names a subscription")
    subs?.close(id)
    if (subs) cb.onSubscribedKinds?.(subs.kinds())
    return {}
  }

  const onRequest = async (
    method: string,
    params: unknown
  ): Promise<unknown> => {
    switch (method) {
      case METHODS.initialize: {
        initialized = true
        cb.onInitialized?.()
        const result: InitializeResult = {
          protocolVersion: PROTOCOL_VERSION,
          hostInfo: HOST_INFO,
          hostCapabilities: {},
          hostContext: cb.hostContext(),
        }
        return result
      }
      case METHODS.ping:
        return {}
      case METHODS.toolsCall:
        return callTool(params)
      case METHODS.subscribe:
        return subscribe(params)
      case METHODS.unsubscribe:
        return unsubscribe(params)
      case METHODS.openLink:
        return openLink(params)
      case METHODS.navigate: {
        const p = obj(params) ?? {}
        const record = obj(p.record)
        const target: NavigateParams = {
          path: typeof p.path === "string" ? p.path : undefined,
          record:
            record && str(record.kind) && str(record.id)
              ? { kind: record.kind as string, id: record.id as string }
              : undefined,
          app: str(p.app),
          back: p.back === true || undefined,
        }
        if (
          target.path === undefined &&
          !target.record &&
          !target.app &&
          !target.back
        ) {
          invalid("navigate names a path, a record, an app or back")
        }
        await cb.onNavigate?.(target)
        return {}
      }
      case METHODS.confirm: {
        requireGranted()
        const p = obj(params) ?? {}
        const title = str(p.title) ?? invalid("confirm carries a title")
        const agreed = await confirm({
          title,
          body: str(p.body),
          destructive: p.destructive === true,
        })
        return { ok: agreed } satisfies ConfirmResult
      }
      default:
        throw new RpcError(ERROR.methodNotFound, `unknown method ${method}`)
    }
  }

  const onNotification = (method: string, params: unknown): void => {
    switch (method) {
      case METHODS.initialized:
        startPinging()
        return
      case METHODS.sizeChanged: {
        const p = obj(params) ?? {}
        cb.onSizeChanged?.({ width: num(p.width), height: num(p.height) })
        return
      }
      case METHODS.primaryAction: {
        if (!opts.granted()) return
        const p = obj(params)
        const label = str(p?.label)
        cb.onPrimaryAction?.(
          label ? { label, enabled: p?.enabled !== false } : null
        )
        return
      }
      case METHODS.title: {
        if (!opts.granted()) return
        const text = str((obj(params) as Partial<TitleParams>)?.text)
        if (text) cb.onTitle?.(text)
        return
      }
      case METHODS.toast: {
        if (!opts.granted()) return
        const p = obj(params)
        const title = str(p?.title)
        if (!title) return
        const type = str(p?.type)
        cb.onToast?.({
          title,
          description: str(p?.description),
          type:
            type === "success" || type === "error" || type === "info"
              ? type
              : undefined,
        })
        return
      }
      case METHODS.error: {
        const p = obj(params)
        const phase = str(p?.phase)
        const message = str(p?.message)
        if (!message) return
        cb.onError?.({
          phase: phase === "transform" || phase === "grant" ? phase : "runtime",
          message,
          path: str(p?.path),
          line: num(p?.line),
          column: num(p?.column),
          module: str(p?.module),
          stack: str(p?.stack),
        })
        return
      }
      default:
        return
    }
  }

  return {
    attach(port) {
      if (rpc) throw new Error("the bridge already has a port")
      rpc = new Endpoint(port, { onRequest, onNotification })
    },
    hostContextChanged(partial) {
      if (!rpc || !initialized || closed) return
      rpc.notify(METHODS.hostContextChanged, partial)
    },
    primaryActionClicked() {
      rpc?.notify(METHODS.primaryActionClicked)
    },
    back() {
      if (!rpc || !initialized || closed) return Promise.resolve(false)
      const asked = rpc
        .call<BackResult>(METHODS.back)
        .then((r) => r?.handled === true)
        .catch(() => false)
      const beat = new Promise<boolean>((resolve) =>
        setTimeout(() => resolve(false), BACK_MS)
      )
      return Promise.race([asked, beat])
    },
    routeChanged(path) {
      if (!rpc || !initialized || closed) return
      rpc.notify(METHODS.routeChanged, { path } satisfies RouteChangedParams)
    },
    teardown() {
      if (closed) return
      if (rpc && initialized) {
        const ending = rpc
        // The request is posted before the port closes; the guest's answer
        // is not waited for, since unmount is synchronous.
        ending.call(METHODS.teardown).catch(() => {})
      }
      close()
    },
    subscribedKinds() {
      return subs?.kinds() ?? []
    },
    get initialized() {
      return initialized
    },
    get closed() {
      return closed
    },
  }
}
