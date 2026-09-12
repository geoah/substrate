/** The bridge's wire. A custom view's document runs in a sandboxed frame on
 * an opaque origin and talks to the console over ONE `MessagePort`, carrying
 * JSON-RPC 2.0 messages as structured-clone objects. The method names are MCP
 * Apps' (SEP-1865, 2026-01-26) wherever that protocol has the word, so a
 * document written here renders under a foreign host with a transport
 * adapter and nothing else; the `substrate/*` methods are what the console's
 * own chrome needs and a foreign host ignores.
 *
 * Both ends share this file, so the SDK chunk carries it; it must therefore
 * import nothing but types. The token never appears here: the guest asks, the
 * host reads with its own credential and answers with data. */

import type { Page, RecordFilter, SubstrateRecord } from "@/lib/api/types"

export const PROTOCOL_VERSION = "2026-01-26"

/** The frame shell announces itself with this to `window.parent`, and the
 * host answers with the one `"*"`-targeted post the boundary allows: the
 * document and the port, sent to the shell the host itself navigated to. */
export const READY_TYPE = "substrate/ready"
export const MOUNT_TYPE = "substrate/mount"

export interface MountMessage {
  type: typeof MOUNT_TYPE
  /** The view's `source`, written into the shell's document. The port
   * travels in the transfer list, not the body. */
  html: string
}

/** How the shell hands the SDK its port after `document.open()` erased every
 * listener the shell had: the SDK asks on `window`, the shell answers with
 * the port in the event's `detail`. Same realm, so the port is not cloned. */
export const PORT_REQUEST_EVENT = "substrate:port-request"
export const PORT_EVENT = "substrate:port"

export const METHODS = {
  initialize: "ui/initialize",
  initialized: "ui/notifications/initialized",
  hostContextChanged: "ui/notifications/host-context-changed",
  toolResult: "ui/notifications/tool-result",
  toolsCall: "tools/call",
  openLink: "ui/open-link",
  sizeChanged: "ui/notifications/size-changed",
  teardown: "ui/resource-teardown",
  ping: "ping",
  navigate: "substrate/navigate",
  primaryAction: "substrate/primary-action",
  primaryActionClicked: "substrate/notifications/primary-action",
  back: "substrate/notifications/back",
} as const

export type Method = (typeof METHODS)[keyof typeof METHODS]

/** The `tools/call` names. The records surface only: functions, agents, the
 * watch and search are later, and GraphQL, tokens, the vocabulary, the
 * catalog, OAuth and blobs are absent, not refused. */
export const TOOLS = {
  list: "substrate.records.list",
  get: "substrate.records.get",
  put: "substrate.records.put",
  patch: "substrate.records.patch",
  delete: "substrate.records.delete",
  transition: "substrate.records.transition",
} as const

export type Tool = (typeof TOOLS)[keyof typeof TOOLS]

// ── JSON-RPC 2.0 framing ────────────────────────────────────────────────────

export type JsonRpcId = number | string

export interface JsonRpcRequest {
  jsonrpc: "2.0"
  id: JsonRpcId
  method: string
  params?: unknown
}

export interface JsonRpcNotification {
  jsonrpc: "2.0"
  method: string
  params?: unknown
}

export interface JsonRpcError {
  code: number
  message: string
  data?: unknown
}

export interface JsonRpcSuccess {
  jsonrpc: "2.0"
  id: JsonRpcId
  result: unknown
}

export interface JsonRpcFailure {
  jsonrpc: "2.0"
  id: JsonRpcId
  error: JsonRpcError
}

export type JsonRpcResponse = JsonRpcSuccess | JsonRpcFailure
export type JsonRpcMessage =
  JsonRpcRequest | JsonRpcNotification | JsonRpcResponse

/** The reserved JSON-RPC codes the bridge uses, and its two of its own in the
 * server-defined range. */
export const ERROR = {
  invalidRequest: -32600,
  methodNotFound: -32601,
  invalidParams: -32602,
  internal: -32603,
  /** The view's grant does not cover the call, or the grant is not the
   * owner's. */
  forbidden: -32001,
  /** The port is closed: torn down, or the guest stopped answering. */
  unavailable: -32002,
} as const

export function request(
  id: JsonRpcId,
  method: string,
  params?: unknown
): JsonRpcRequest {
  return params === undefined
    ? { jsonrpc: "2.0", id, method }
    : { jsonrpc: "2.0", id, method, params }
}

export function notification(
  method: string,
  params?: unknown
): JsonRpcNotification {
  return params === undefined
    ? { jsonrpc: "2.0", method }
    : { jsonrpc: "2.0", method, params }
}

export function success(id: JsonRpcId, result: unknown): JsonRpcSuccess {
  return { jsonrpc: "2.0", id, result: result ?? {} }
}

export function failure(
  id: JsonRpcId,
  code: number,
  message: string,
  data?: unknown
): JsonRpcFailure {
  return {
    jsonrpc: "2.0",
    id,
    error: data === undefined ? { code, message } : { code, message, data },
  }
}

function isId(v: unknown): v is JsonRpcId {
  return typeof v === "number" || typeof v === "string"
}

export function isRequest(m: unknown): m is JsonRpcRequest {
  const o = m as Partial<JsonRpcRequest> | null
  return (
    Boolean(o) &&
    typeof o === "object" &&
    o!.jsonrpc === "2.0" &&
    typeof o!.method === "string" &&
    isId(o!.id)
  )
}

export function isNotification(m: unknown): m is JsonRpcNotification {
  const o = m as Partial<JsonRpcRequest> | null
  return (
    Boolean(o) &&
    typeof o === "object" &&
    o!.jsonrpc === "2.0" &&
    typeof o!.method === "string" &&
    !("id" in o!)
  )
}

export function isResponse(m: unknown): m is JsonRpcResponse {
  const o = m as Partial<JsonRpcSuccess & JsonRpcFailure> | null
  return (
    Boolean(o) &&
    typeof o === "object" &&
    o!.jsonrpc === "2.0" &&
    isId(o!.id) &&
    !("method" in o!) &&
    ("result" in o! || "error" in o!)
  )
}

/** One message off the port. A string is parsed as JSON so a transport
 * adapter that serializes still frames; anything that is not one of the
 * three shapes is dropped, never answered, because an unframed message has
 * no id to answer to. */
export function parseMessage(data: unknown): JsonRpcMessage | undefined {
  let m = data
  if (typeof m === "string") {
    try {
      m = JSON.parse(m)
    } catch {
      return undefined
    }
  }
  if (isRequest(m) || isNotification(m) || isResponse(m)) return m
  return undefined
}

// ── the shapes each method carries ──────────────────────────────────────────

export type Theme = "light" | "dark"
export type DisplayMode = "inline" | "fullscreen"

export interface HostContext {
  theme: Theme
  displayMode: DisplayMode
  containerDimensions: { width: number; height: number }
  /** `env()` is zero inside an iframe, so the host measures and forwards. */
  safeAreaInsets: { top: number; right: number; bottom: number; left: number }
  platform: "web"
  locale: string
  timeZone: string
  styles: {
    /** The console's tokens (`--background`, `--primary`, `--radius`, …), set
     * on the guest's `:root` by the SDK. */
    variables: Record<string, string>
    /** The console font's `@font-face` rules, verbatim, so a guest may draw
     * in it; the file loads only where the console origin answers CORS. */
    fontFaces: string[]
  }
}

export interface InitializeParams {
  protocolVersion?: string
  appInfo?: { name?: string; version?: string }
}

export interface InitializeResult {
  protocolVersion: string
  hostInfo: { name: string; version: string }
  hostCapabilities: Record<string, unknown>
  hostContext: HostContext
}

export interface CallToolParams {
  name: string
  arguments?: Record<string, unknown>
}

/** MCP's tool result: an execution failure is `isError` with its text, a
 * protocol or grant failure is a JSON-RPC error instead. */
export interface CallToolResult {
  content: { type: "text"; text: string }[]
  structuredContent?: Record<string, unknown>
  isError?: boolean
}

/** What the host pushes: the view's own page. */
export type RecordsPush = {
  records: SubstrateRecord[]
  head?: number
}

export interface ListArguments {
  kind: string
  filter?: RecordFilter
  orderBy?: string
  first?: number
  after?: string
}

export type ListResult = Pick<Page, "records" | "cursor" | "head">

export interface OpenLinkParams {
  url: string
}

export interface SizeChangedParams {
  width?: number
  height?: number
}

export interface NavigateParams {
  view?: string
  record?: { kind: string; id: string }
}

export interface PrimaryActionParams {
  label: string
  enabled?: boolean
}

// ── the endpoint both ends run ──────────────────────────────────────────────

export class RpcError extends Error {
  code: number
  data?: unknown
  constructor(code: number, message: string, data?: unknown) {
    super(message)
    this.name = "RpcError"
    this.code = code
    this.data = data
  }
}

export interface EndpointHandlers {
  /** Answer a request; throw an `RpcError` to fail it with a code, anything
   * else fails it as `internal`. */
  onRequest: (method: string, params: unknown) => unknown
  onNotification: (method: string, params: unknown) => void
}

interface Pending {
  resolve: (v: unknown) => void
  reject: (e: Error) => void
}

/** One side of the port: sends requests and notifications, answers the
 * other side's, and settles its own pending calls. Closing rejects every
 * pending call as `unavailable` so a caller never hangs on a dead port. */
export class Endpoint {
  private next = 1
  private pending = new Map<JsonRpcId, Pending>()
  private closed = false
  private port: MessagePort
  private handlers: EndpointHandlers

  constructor(port: MessagePort, handlers: EndpointHandlers) {
    this.port = port
    this.handlers = handlers
    port.onmessage = (e: MessageEvent) => this.receive(e.data)
  }

  call<T = unknown>(method: string, params?: unknown): Promise<T> {
    if (this.closed) {
      return Promise.reject(
        new RpcError(ERROR.unavailable, "the bridge is closed")
      )
    }
    const id = this.next++
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as Pending["resolve"], reject })
      this.port.postMessage(request(id, method, params))
    })
  }

  notify(method: string, params?: unknown): void {
    if (this.closed) return
    this.port.postMessage(notification(method, params))
  }

  close(): void {
    if (this.closed) return
    this.closed = true
    for (const p of this.pending.values()) {
      p.reject(new RpcError(ERROR.unavailable, "the bridge is closed"))
    }
    this.pending.clear()
    this.port.onmessage = null
    this.port.close()
  }

  get isClosed(): boolean {
    return this.closed
  }

  private receive(data: unknown): void {
    if (this.closed) return
    const m = parseMessage(data)
    if (!m) return
    if (isResponse(m)) {
      const p = this.pending.get(m.id)
      if (!p) return
      this.pending.delete(m.id)
      if ("error" in m) {
        p.reject(new RpcError(m.error.code, m.error.message, m.error.data))
      } else {
        p.resolve(m.result)
      }
      return
    }
    if (isRequest(m)) {
      Promise.resolve()
        .then(() => this.handlers.onRequest(m.method, m.params))
        .then(
          (result) => {
            if (!this.closed) this.port.postMessage(success(m.id, result))
          },
          (err: unknown) => {
            if (this.closed) return
            const e =
              err instanceof RpcError
                ? err
                : new RpcError(
                    ERROR.internal,
                    err instanceof Error ? err.message : String(err)
                  )
            this.port.postMessage(failure(m.id, e.code, e.message, e.data))
          }
        )
      return
    }
    this.handlers.onNotification(m.method, m.params)
  }
}
