/** `substrate/app`: what a custom view's document imports. It is emitted as
 * its own chunk so the frame shell's import map can name it: in dev the map
 * points at `/src/apps-sdk/index.ts` and vite transforms it on request; in a
 * build at the chunk's CONTENT-HASHED name, which the bundler writes into the
 * shell (vite.config.ts, `sdkUrl`), because the shell cannot read a manifest
 * from an origin whose `connect-src` is `'none'`, and `/assets/` is cached
 * immutable, where a fixed name would outlive its build. The guest fetches it
 * cross-origin, so a built console serves it only once the Go server answers
 * CORS on `/assets` (docs/plans/apps.md); vite dev already does.
 *
 * No injected global: the document reaches the console only through the
 * object `createApp()` resolves, and that object reaches the console only
 * through the port the shell handed over. There is no `fetch` here, no
 * token, no GraphQL and no vocabulary; a method that is not on `App` is
 * absent, not refused. The SDK imports `protocol.ts` and nothing else, so
 * the chunk stays small and carries no console code. */

import {
  Endpoint,
  METHODS,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  PROTOCOL_VERSION,
  RpcError,
  TOOLS,
  type CallToolResult,
  type HostContext,
  type InitializeResult,
  type ListArguments,
  type ListResult,
  type NavigateParams,
  type PrimaryActionParams,
  type RecordsPush,
} from "@/lib/apps/bridge/protocol"
import type { RecordFilter, SubstrateRecord } from "@/lib/api/types"

export type { HostContext, RecordFilter, SubstrateRecord }

export interface PutArguments {
  id?: string
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
  ifVersion?: number
  /** A create retried with the same key lands once. */
  idempotencyKey?: string
}

export interface PatchArguments {
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
  ifVersion?: number
}

export interface Records {
  list(args: ListArguments): Promise<ListResult>
  get(kind: string, id: string): Promise<SubstrateRecord>
  put(kind: string, args: PutArguments): Promise<SubstrateRecord>
  patch(
    kind: string,
    id: string,
    args: PatchArguments
  ): Promise<SubstrateRecord>
  delete(kind: string, id: string, ifVersion?: number): Promise<void>
  /** A state move; the host checks the declared machine before the PATCH. */
  transition(
    kind: string,
    id: string,
    property: string | undefined,
    to: string,
    opts?: { ifVersion?: number }
  ): Promise<SubstrateRecord>
}

export type Unsubscribe = () => void

export interface App {
  readonly hostContext: HostContext
  onHostContextChanged(cb: (ctx: HostContext) => void): Unsubscribe
  /** The view's own page, as the host pushes it: once on subscribe if a page
   * has arrived, and on every push after. */
  onRecords(
    cb: (records: SubstrateRecord[], meta: { head?: number }) => void
  ): Unsubscribe
  readonly records: Records
  navigate(target: NavigateParams): Promise<void>
  /** http, https or mailto, opened by the host after it asks the person. */
  openLink(url: string): Promise<void>
  readonly primaryAction: {
    set(state: PrimaryActionParams | null): void
    onClick(cb: () => void): Unsubscribe
  }
  readonly back: {
    onClick(cb: () => void): Unsubscribe
  }
  /** Tell the host how tall the document is; honored in a card, capped. */
  notifySizeChanged(): void
}

const FONT_STYLE_ID = "substrate-fonts"

/** The tokens land on `:root` so `var(--primary)` resolves in the guest's
 * own CSS; the theme lands as the `dark` class the console uses, so a guest
 * stylesheet's `.dark` arm matches too. */
function applyStyles(ctx: HostContext): void {
  const root = document.documentElement
  for (const [name, value] of Object.entries(ctx.styles?.variables ?? {})) {
    root.style.setProperty(name, value)
  }
  root.classList.toggle("dark", ctx.theme === "dark")
  root.style.colorScheme = ctx.theme
  const faces = ctx.styles?.fontFaces ?? []
  let style = document.getElementById(FONT_STYLE_ID)
  if (faces.length && !style) {
    style = document.createElement("style")
    style.id = FONT_STYLE_ID
    document.head.appendChild(style)
  }
  if (style) style.textContent = faces.join("\n")
}

/** The shell keeps the port in code that survived `document.open()`; ask it. */
function acquirePort(): Promise<MessagePort> {
  return new Promise((resolve) => {
    const onPort = (e: Event) => {
      const port = (e as CustomEvent<unknown>).detail
      if (port instanceof MessagePort) {
        window.removeEventListener(PORT_EVENT, onPort)
        resolve(port)
      }
    }
    window.addEventListener(PORT_EVENT, onPort)
    window.dispatchEvent(new Event(PORT_REQUEST_EVENT))
  })
}

/** Element boxes, not `scrollHeight`, which is never smaller than the frame
 * and so could never report a document that shrank. */
function measure(): { width: number; height: number } {
  const html = document.documentElement.getBoundingClientRect()
  const body = document.body?.getBoundingClientRect()
  return {
    width: Math.ceil(html.width),
    height: Math.ceil(Math.max(html.height, body?.bottom ?? 0)),
  }
}

class Listeners<T extends unknown[]> {
  private set = new Set<(...args: T) => void>()
  add(cb: (...args: T) => void): Unsubscribe {
    this.set.add(cb)
    return () => this.set.delete(cb)
  }
  fire(...args: T): void {
    for (const cb of [...this.set]) cb(...args)
  }
}

function unwrap<T>(result: CallToolResult): T {
  if (result.isError) {
    throw new Error(result.content?.[0]?.text ?? "the call failed")
  }
  return result.structuredContent as T
}

async function connect(): Promise<App> {
  const port = await acquirePort()

  let context: HostContext | undefined
  let page: RecordsPush | undefined
  let tornDown = false
  const contextListeners = new Listeners<[HostContext]>()
  const recordsListeners = new Listeners<
    [SubstrateRecord[], { head?: number }]
  >()
  const primaryListeners = new Listeners<[]>()
  const backListeners = new Listeners<[]>()

  const rpc = new Endpoint(port, {
    onRequest(method) {
      switch (method) {
        case METHODS.ping:
          return {}
        case METHODS.teardown:
          tornDown = true
          queueMicrotask(() => rpc.close())
          return {}
        default:
          throw new RpcError(-32601, `unknown method ${method}`)
      }
    },
    onNotification(method, params) {
      switch (method) {
        case METHODS.hostContextChanged: {
          if (!context) return
          context = { ...context, ...(params as Partial<HostContext>) }
          applyStyles(context)
          contextListeners.fire(context)
          return
        }
        case METHODS.toolResult: {
          const result = params as CallToolResult
          const pushed = result?.structuredContent as RecordsPush | undefined
          if (!pushed || !Array.isArray(pushed.records)) return
          page = pushed
          recordsListeners.fire(pushed.records, { head: pushed.head })
          return
        }
        case METHODS.primaryActionClicked:
          primaryListeners.fire()
          return
        case METHODS.back:
          backListeners.fire()
          return
        default:
          return
      }
    },
  })

  const init = await rpc.call<InitializeResult>(METHODS.initialize, {
    protocolVersion: PROTOCOL_VERSION,
    appInfo: { name: "substrate-app-sdk", version: "0" },
  })
  context = init.hostContext
  applyStyles(context)
  rpc.notify(METHODS.initialized)

  const callTool = async <T>(
    name: string,
    args: Record<string, unknown>
  ): Promise<T> => {
    if (tornDown) throw new Error("the view was torn down")
    const result = await rpc.call<CallToolResult>(METHODS.toolsCall, {
      name,
      arguments: args,
    })
    return unwrap<T>(result)
  }

  const records: Records = {
    list: (args) => callTool<ListResult>(TOOLS.list, { ...args }),
    get: (kind, id) => callTool<SubstrateRecord>(TOOLS.get, { kind, id }),
    put: (kind, args) =>
      callTool<SubstrateRecord>(TOOLS.put, { kind, ...args }),
    patch: (kind, id, args) =>
      callTool<SubstrateRecord>(TOOLS.patch, { kind, id, ...args }),
    delete: async (kind, id, ifVersion) => {
      await callTool<unknown>(TOOLS.delete, { kind, id, ifVersion })
    },
    transition: (kind, id, property, to, opts = {}) =>
      callTool<SubstrateRecord>(TOOLS.transition, {
        kind,
        id,
        property,
        to,
        ifVersion: opts.ifVersion,
      }),
  }

  return {
    get hostContext() {
      return context!
    },
    onHostContextChanged: (cb) => contextListeners.add(cb),
    onRecords(cb) {
      const off = recordsListeners.add(cb)
      if (page) cb(page.records, { head: page.head })
      return off
    },
    records,
    async navigate(target) {
      await rpc.call(METHODS.navigate, target)
    },
    async openLink(url) {
      await rpc.call(METHODS.openLink, { url })
    },
    primaryAction: {
      set: (state) => rpc.notify(METHODS.primaryAction, state),
      onClick: (cb) => primaryListeners.add(cb),
    },
    back: {
      onClick: (cb) => backListeners.add(cb),
    },
    notifySizeChanged: () => rpc.notify(METHODS.sizeChanged, measure()),
  }
}

let app: Promise<App> | undefined

/** One app per document: the port has one owner, so a second call shares the
 * first's connection rather than racing it for the port. */
export function createApp(): Promise<App> {
  app ??= connect()
  return app
}
