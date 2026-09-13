/** The console's end of the bridge: one host per mounted custom view. It
 * holds the port, answers `ui/initialize` with the host context, pushes the
 * view's own page and re-pushes it whenever the page changes, serves
 * `tools/call` for the records surface with the console's OWN credential,
 * and pings the guest so a document that stopped answering is torn down
 * rather than left drawing stale data.
 *
 * Every read and every write is checked against the view's grant by RESOLVED
 * kind identity: the name the guest sends is looked up in the registry and
 * the grant is asked about what was found, so a spelling that is not a kind
 * is refused as unknown rather than matched as a string. The pushed page is
 * held to the same grant. And the grant counts only when the component says
 * so (`provenance`): below owner provenance the guest gets no data and no
 * calls, whatever the row's `permissions` say, because an agent that may
 * write views could have widened them under an owner-written `source`; and
 * a row without an owner-written `filter` gets calls but no pushed page,
 * because the page's selection is the one thing the filter approves. */

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
import { collectionPath, request, seg } from "@/lib/api/http"
import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { admits, stateSpecOf } from "../machine"
import type { ViewSpec } from "../spec"
import {
  Endpoint,
  ERROR,
  METHODS,
  PROTOCOL_VERSION,
  RpcError,
  TOOLS,
  type CallToolParams,
  type CallToolResult,
  type HostContext,
  type InitializeResult,
  type ListArguments,
  type NavigateParams,
  type PrimaryActionParams,
  type RecordsPush,
  type SizeChangedParams,
} from "./protocol"

/** How often the guest is asked whether it is still there, and how many
 * unanswered asks end the view. */
export const PING_MS = 5_000
export const PING_MISSES = 3

const HOST_INFO = { name: "substrate-console", version: "0" }

/** The schemes `ui/open-link` may open. Anything else is refused before the
 * person is asked. */
const LINK_SCHEMES = new Set(["http:", "https:", "mailto:"])

/** The properties a custom view must carry, each written at owner tier,
 * before its document runs: the code, what it may reach, and what it is a
 * view of. */
const REQUIRED = ["source", "permissions", "kind"] as const

export interface Provenance {
  /** The document runs and the grant is honored. */
  granted: boolean
  /** The row carries an owner-written `filter`, so the view's own page may
   * be pushed. Without one the page's selection was approved by nobody, and
   * the host pushes none; the document may still call `records.list`, which
   * the grant checks. */
  filtered: boolean
}

export const NOT_GRANTED: Provenance = { granted: false, filtered: false }

/** What the single-record read's `propertyMeta` says about the row (lists
 * omit it). An ABSENT required property is a refusal, not a pass: an agent
 * allowed to write views can delete a property as well as change one, and a
 * row without the meta, written before the tiers, is below owner, not above
 * it. `filter` alone may be absent, and then only the page is withheld. */
export function ownerProvenance(
  record: SubstrateRecord | undefined
): Provenance {
  if (!record || typeof record.properties.source !== "string") {
    return NOT_GRANTED
  }
  const meta = record.propertyMeta ?? {}
  const owner = (p: string) =>
    record.properties[p] !== undefined && meta[p]?.tier === "owner"
  if (!REQUIRED.every(owner)) return NOT_GRANTED
  if (record.properties.filter === undefined) {
    return { granted: true, filtered: false }
  }
  return owner("filter") ? { granted: true, filtered: true } : NOT_GRANTED
}

export type Access =
  { ok: true; kind: KindInfo } | { ok: false; message: string }

export type Grant = Pick<ViewSpec, "permissions">

/** The one gate every call goes through. */
export function checkAccess(
  grant: Grant,
  kinds: KindInfo[],
  op: "read" | "write",
  kind: unknown
): Access {
  if (typeof kind !== "string" || !kind) {
    return { ok: false, message: "the call names no kind" }
  }
  const resolved = kindByIdentity(kinds, kind)
  if (!resolved) return { ok: false, message: `unknown kind ${kind}` }
  const allowed =
    op === "read"
      ? grant.permissions.reads.kinds.includes(resolved.identity)
      : grant.permissions.writes.includes(resolved.identity)
  if (!allowed) {
    return {
      ok: false,
      message: `the view's grant does not ${op} ${resolved.identity}`,
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

export interface HostCallbacks {
  hostContext: () => HostContext
  onInitialized?: () => void
  onSizeChanged?: (size: SizeChangedParams) => void
  onNavigate?: (target: NavigateParams) => void | Promise<void>
  onPrimaryAction?: (state: PrimaryActionParams | null) => void
  /** Ask the person before a link opens; the default is the browser's own
   * confirm. */
  confirmLink?: (url: URL) => boolean | Promise<boolean>
  openLink?: (url: URL) => void
  /** The guest missed too many pings; the host has already torn down. */
  onLost?: () => void
  /** The guest's document left its frame (`substrate/notifications/unload`,
   * posted by the shell from `pagehide`); the host has already torn down. */
  onUnloaded?: () => void
}

export interface BridgeHostOptions {
  /** Read on every call, so a view saved while mounted is checked against
   * its new grant and not the one it mounted with. The spec here and the
   * provenance below must come from the SAME read of the row: a grant
   * checked against one snapshot and approved by another is no approval. */
  view: () => { spec: ViewSpec; kinds: KindInfo[] }
  /** What the owner wrote (see `ownerProvenance`). */
  provenance: () => Provenance
  callbacks: HostCallbacks
  api?: RecordsApi
  pingMs?: number
}

export interface BridgeHost {
  attach(port: MessagePort): void
  /** The view's own page; pushed now if the guest is ready, else when it is.
   * Withheld unless the grant reads the view's kind. */
  pushPage(page: Page | undefined): void
  hostContextChanged(partial: Partial<HostContext>): void
  primaryActionClicked(): void
  back(): void
  /** `ui/resource-teardown`, then the port closes. Idempotent. */
  teardown(): void
  readonly initialized: boolean
  readonly closed: boolean
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
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

function toolError(err: unknown): CallToolResult {
  return {
    isError: true,
    content: [
      { type: "text", text: err instanceof Error ? err.message : String(err) },
    ],
  }
}

export function createBridgeHost(opts: BridgeHostOptions): BridgeHost {
  const api = opts.api ?? liveRecordsApi
  const pingMs = opts.pingMs ?? PING_MS
  const cb = opts.callbacks
  let rpc: Endpoint | undefined
  let initialized = false
  let ready = false
  let closed = false
  let page: Page | undefined
  let pinger: ReturnType<typeof setInterval> | undefined
  let misses = 0
  let awaitingPong = false

  const grantFor = (op: "read" | "write", kind: unknown): KindInfo => {
    if (!opts.provenance().granted) {
      throw new RpcError(
        ERROR.forbidden,
        "the view's grant has not been written by the owner"
      )
    }
    const { spec, kinds } = opts.view()
    const access = checkAccess(spec, kinds, op, kind)
    if (!access.ok) throw new RpcError(ERROR.forbidden, access.message)
    return access.kind
  }

  const pushNow = () => {
    if (!rpc || !ready || !page) return
    const { granted, filtered } = opts.provenance()
    if (!granted || !filtered) return
    const { spec, kinds } = opts.view()
    if (!spec.kind) return
    if (!checkAccess(spec, kinds, "read", spec.kind).ok) return
    const structuredContent: RecordsPush = {
      records: page.records ?? [],
      head: page.head,
    }
    rpc.notify(METHODS.toolResult, {
      content: [],
      structuredContent,
    } satisfies CallToolResult)
  }

  const stopPinging = () => {
    if (pinger !== undefined) clearInterval(pinger)
    pinger = undefined
  }

  const close = () => {
    if (closed) return
    closed = true
    stopPinging()
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

  const callTool = async (params: unknown): Promise<CallToolResult> => {
    const p = obj(params) as Partial<CallToolParams> | undefined
    const name = str(p?.name) ?? invalid("tools/call names a tool")
    const args = obj(p?.arguments) ?? {}
    switch (name) {
      case TOOLS.list: {
        const kind = grantFor("read", args.kind)
        const a = args as unknown as ListArguments
        const { authority, pkg, name: kindName } = parts(kind)
        try {
          const result = await api.list({
            authority,
            package: pkg,
            name: kindName,
            first: num(a.first),
            after: str(a.after),
            filter: obj(a.filter),
            orderBy: str(a.orderBy),
          })
          return ok({
            records: result.records ?? [],
            cursor: result.cursor,
            head: result.head,
          })
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
        const state = stateSpecOf(kind, str(args.property))
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
          const current = await api.get(kind, id)
          const from = str(current.properties[state.name])
          if (!admits(kind, state.name, from, to)) {
            return toolError(
              new Error(
                `${state.name} does not move from ${from ?? "unset"} to ${to}`
              )
            )
          }
          const record = await api.patch(kind, id, {
            properties: { [state.name]: to },
            ifVersion: num(args.ifVersion) ?? current.version,
          })
          return ok(record as unknown as Record<string, unknown>)
        } catch (err) {
          return toolError(err)
        }
      }
      default:
        throw new RpcError(ERROR.methodNotFound, `unknown tool ${name}`)
    }
  }

  const openLink = async (params: unknown): Promise<Record<string, never>> => {
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
    const confirm =
      cb.confirmLink ?? ((u: URL) => window.confirm(`Open ${u.href}?`))
    if (!(await confirm(url))) {
      throw new RpcError(ERROR.forbidden, "the person declined")
    }
    const open =
      cb.openLink ??
      ((u: URL) => {
        window.open(u.href, "_blank", "noopener,noreferrer")
      })
    open(url)
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
      case METHODS.openLink:
        return openLink(params)
      case METHODS.navigate: {
        const p = obj(params) ?? {}
        const record = obj(p.record)
        const target: NavigateParams = {
          view: str(p.view),
          record:
            record && str(record.kind) && str(record.id)
              ? { kind: record.kind as string, id: record.id as string }
              : undefined,
        }
        if (!target.view && !target.record)
          invalid("navigate names a view or a record")
        await cb.onNavigate?.(target)
        return {}
      }
      default:
        throw new RpcError(ERROR.methodNotFound, `unknown method ${method}`)
    }
  }

  const onNotification = (method: string, params: unknown): void => {
    switch (method) {
      case METHODS.initialized:
        ready = true
        startPinging()
        pushNow()
        return
      case METHODS.sizeChanged: {
        const p = obj(params) ?? {}
        cb.onSizeChanged?.({ width: num(p.width), height: num(p.height) })
        return
      }
      case METHODS.primaryAction: {
        const p = obj(params)
        const label = str(p?.label)
        cb.onPrimaryAction?.(
          label ? { label, enabled: p?.enabled !== false } : null
        )
        return
      }
      case METHODS.unload:
        // The frame is committing another document. What it may commit is
        // the browser's error page (the console's `frame-src 'self'`), and
        // nothing this port reaches is on the other side of it.
        if (closed) return
        close()
        cb.onUnloaded?.()
        return
      default:
        return
    }
  }

  return {
    attach(port) {
      if (rpc) throw new Error("the bridge already has a port")
      rpc = new Endpoint(port, { onRequest, onNotification })
    },
    pushPage(next) {
      page = next
      pushNow()
    },
    hostContextChanged(partial) {
      if (!rpc || !initialized || closed) return
      rpc.notify(METHODS.hostContextChanged, partial)
    },
    primaryActionClicked() {
      rpc?.notify(METHODS.primaryActionClicked)
    },
    back() {
      rpc?.notify(METHODS.back)
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
    get initialized() {
      return initialized
    },
    get closed() {
      return closed
    },
  }
}
