/** The imperative SDK: what `createApp()` resolves. Every records call is an
 * RPC the host answers with its own credential; the declarations and the
 * resolved inputs arrive once in `hostContext`, and `kinds`/`inputs` read
 * them locally. There is no `fetch` here, no token, no GraphQL and no
 * vocabulary; a method that is not on `App` is absent, not refused.
 *
 * No injected global and NO React: the document reaches the console only
 * through the object `createApp()` resolves, and that object reaches the
 * console only through the port the shell handed over. `hooks.ts` is the
 * React layer over this file; `index.ts` is the `substrate/app` entry that
 * exports both. */

import type { AgentEvent } from "@/lib/api/agents"
import type {
  ProblemDetail,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  Endpoint,
  METHODS,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  PROTOCOL_VERSION,
  RpcError,
  ERROR,
  TOOLS,
  type AgentEventPush,
  type BackResult,
  type CallToolResult,
  type FunctionCalledResult,
  type GuestError,
  type HostContext,
  type InitializeResult,
  type InputState,
  type ListResult,
  type NavigateParams,
  type PrimaryActionParams,
  type RecordsPush,
  type SubscribeResult,
  type ToolFailure,
} from "@/lib/apps/bridge/protocol"
import { declarationOf, type Declaration } from "./declaration"

export type {
  AgentEvent,
  Declaration,
  GuestError,
  HostContext,
  InputState,
  RecordFilter,
  SubstrateRecord,
}
export type { PropSpec, Arm, EnumValue } from "./declaration"

/** A read: one kind, several, or every implementor of a trait. */
export type Query = (
  { kind: string } | { kinds: string[] } | { implements: string }
) & {
  filter?: RecordFilter
  orderBy?: string
  first?: number
  after?: string
}

export interface Page {
  records: SubstrateRecord[]
  cursor?: string
  /** A fan-out stopped at one page per kind and one had more. */
  incomplete?: boolean
  loading: boolean
  error?: SdkError
}

/** The API's error as the guest sees it: `code` is the wire's closed set
 * (`conflict`, `validation`, `forbidden`, `not_found`, `network`, …) plus
 * `unavailable` for a torn-down bridge. */
export class SdkError extends Error {
  code: string
  problems?: string[]
  problemDetails?: ProblemDetail[]
  constructor(failure: ToolFailure) {
    super(failure.message)
    this.name = "SdkError"
    this.code = failure.code
    this.problems = failure.problems
    this.problemDetails = failure.problemDetails
  }
}

export type Unsubscribe = () => void

export interface PutArguments {
  id?: string
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
}

export interface PatchArguments {
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
}

export interface Records {
  list(q: Query): Promise<Page>
  get(kind: string, id: string): Promise<SubstrateRecord>
  /** With no `id` a create; the idempotency key is minted once per call and
   * a `network` failure is retried once under it. */
  put(
    kind: string,
    args: PutArguments,
    opts?: { ifVersion?: number; idempotencyKey?: string }
  ): Promise<SubstrateRecord>
  patch(
    kind: string,
    id: string,
    args: PatchArguments,
    opts?: { ifVersion?: number }
  ): Promise<SubstrateRecord>
  delete(kind: string, id: string, opts?: { ifVersion?: number }): Promise<void>
  /** A state move; the host checks the declared machine before the patch.
   * `property` undefined means the kind's sole state property. */
  transition(
    kind: string,
    id: string,
    property: string | undefined,
    to: string,
    opts?: { ifVersion?: number }
  ): Promise<SubstrateRecord>
  /** The first page now and a new page every time the tail moves it. */
  subscribe(q: Query, cb: (page: Page) => void): Unsubscribe
}

export interface Functions {
  call(
    ref: string,
    args: Record<string, unknown>,
    opts?: { idempotencyKey?: string }
  ): Promise<{ output: unknown; effects: number }>
}

export interface Agents {
  chat(
    ref: string,
    args: { thread?: string; message: string },
    onEvent: (e: AgentEvent) => void
  ): { stop(): void }
}

export type NavigateTarget =
  | { path: string }
  | { record: SubstrateRecord | { kind: string; id: string } }
  | { app: string; path?: string }

export interface Host {
  title(text: string): void
  navigate(target: NavigateTarget): Promise<void>
  /** The host's own back: history while the app's path is non-empty, the
   * launcher when it is. */
  back(): void
  /** http, https, mailto or tel, opened by the host after it asks. */
  openLink(url: string): Promise<void>
  primaryAction: {
    set(state: PrimaryActionParams | null): void
    onClick(cb: () => void): Unsubscribe
  }
  backButton: {
    /** The host's back button was tapped. A listener makes the tap the
     * guest's to handle; without one the host navigates. */
    onClick(cb: () => void): Unsubscribe
  }
  confirm(args: {
    title: string
    body?: string
    destructive?: boolean
  }): Promise<boolean>
  toast(args: {
    title: string
    description?: string
    type?: "success" | "error" | "info"
  }): void
  /** Tell the host how tall the document is; honored in a card, capped. The
   * SDK reports on its own whenever the body resizes in a card. */
  notifySizeChanged(): void
}

export interface Route {
  readonly path: string
  navigate(path: string): void
  onChange(cb: (path: string) => void): Unsubscribe
}

export interface App {
  readonly hostContext: HostContext
  onHostContextChanged(cb: (ctx: HostContext) => void): Unsubscribe
  readonly records: Records
  readonly functions: Functions
  readonly agents: Agents
  readonly host: Host
  readonly route: Route
  /** The declarations the grant covers; local, no call. */
  readonly kinds: {
    get(identity: string): Declaration | undefined
    list(): Declaration[]
  }
  /** The resolved inputs; local, no call. */
  readonly inputs: { get(name: string): InputState }
  subscribe: Records["subscribe"]
}

/** What an unresolved input says on the screen; undefined once it resolves.
 * The console's `lib/apps/inputs.ts` says the same words. */
export function inputStatus(
  name: string,
  state: InputState,
  kindName?: string
): string | undefined {
  const noun = kindName ?? "record"
  switch (state.state) {
    case "missing":
      return `\`${name}\` is bound to ${state.binding}, which no longer exists`
    case "none":
      return `no ${noun} connected yet for \`${name}\``
    case "unknown-kind":
      return `\`${name}\` needs a kind this repository does not have`
    case "ambiguous":
      return `pick which ${noun} is \`${name}\``
    default:
      return undefined
  }
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
  get size(): number {
    return this.set.size
  }
}

/** A JSON-RPC failure as an `SdkError`: the bridge's own codes read as
 * `forbidden`/`unavailable`, anything else as `internal`. */
function sdkError(err: unknown): SdkError {
  if (err instanceof SdkError) return err
  if (err instanceof RpcError) {
    const code =
      err.code === ERROR.forbidden
        ? "forbidden"
        : err.code === ERROR.unavailable
          ? "unavailable"
          : err.code === ERROR.invalidParams
            ? "bad_request"
            : "internal"
    return new SdkError({ code, message: err.message })
  }
  return new SdkError({
    code: "internal",
    message: err instanceof Error ? err.message : String(err),
  })
}

function unwrap<T>(result: CallToolResult): T {
  if (result.isError) {
    const failure = result.structuredContent as ToolFailure | undefined
    throw new SdkError(
      failure?.code
        ? failure
        : {
            code: "internal",
            message: result.content?.[0]?.text ?? "the call failed",
          }
    )
  }
  return result.structuredContent as T
}

function pageOf(page: RecordsPush["page"] | ListResult): Page {
  const error = (page as RecordsPush["page"]).error
  return {
    records: page.records ?? [],
    cursor: page.cursor,
    incomplete: page.incomplete,
    loading: false,
    error: error ? new SdkError(error) : undefined,
  }
}

/** The name of the module a `blob:` URL was minted for, read back off the
 * import map the shell wrote (`#name` per module, `#source` for the entry),
 * so an error's frame names the author's module. */
export function moduleNameOf(url: string | undefined): string | undefined {
  if (!url) return undefined
  const map = document.querySelector('script[type="importmap"]')
  if (!map?.textContent) return undefined
  try {
    const imports =
      (JSON.parse(map.textContent) as { imports?: Record<string, string> })
        .imports ?? {}
    for (const [name, target] of Object.entries(imports)) {
      if (target === url && name.startsWith("#")) return name.slice(1)
    }
  } catch {
    return undefined
  }
  return undefined
}

/** The first frame of a stack that names one of the app's own modules. */
export function locate(
  stack: string | undefined
): Pick<GuestError, "module" | "line" | "column"> {
  if (!stack) return {}
  const m = stack.match(/(blob:[^\s):]+(?::[^\s):]+)?):(\d+):(\d+)/)
  if (!m) return {}
  return {
    module: moduleNameOf(m[1]),
    line: Number(m[2]),
    column: Number(m[3]),
  }
}

let endpoint: Endpoint | undefined
let connected: App | undefined

/** Tell the host something went wrong in the guest. Before the bridge is up
 * there is nobody to tell, and the shell reports for itself. */
export function reportError(error: GuestError): void {
  endpoint?.notify(METHODS.error, error)
}

async function connect(): Promise<App> {
  const port = await acquirePort()

  let context: HostContext | undefined
  let tornDown = false
  let path = ""
  const contextListeners = new Listeners<[HostContext]>()
  const primaryListeners = new Listeners<[]>()
  const backListeners = new Listeners<[]>()
  const routeListeners = new Listeners<[string]>()
  const subscriptions = new Map<string, (page: Page) => void>()
  const streams = new Map<string, (e: AgentEvent) => void>()
  const declarations = new WeakMap<object, Declaration>()

  const rpc = new Endpoint(port, {
    onRequest(method) {
      switch (method) {
        case METHODS.ping:
          return {}
        case METHODS.back: {
          backListeners.fire()
          return { handled: backListeners.size > 0 } satisfies BackResult
        }
        case METHODS.teardown:
          tornDown = true
          queueMicrotask(() => rpc.close())
          return {}
        default:
          throw new RpcError(ERROR.methodNotFound, `unknown method ${method}`)
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
        case METHODS.records: {
          const push = params as RecordsPush
          subscriptions.get(push?.subscription)?.(pageOf(push.page))
          return
        }
        case METHODS.agentEvent: {
          const push = params as AgentEventPush
          streams.get(push?.stream)?.(push.event)
          return
        }
        case METHODS.routeChanged: {
          const next = (params as { path?: string })?.path ?? ""
          if (next === path) return
          path = next
          routeListeners.fire(path)
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
  endpoint = rpc

  const init = await rpc.call<InitializeResult>(METHODS.initialize, {
    protocolVersion: PROTOCOL_VERSION,
    appInfo: { name: "substrate-app-sdk", version: "1" },
  })
  context = init.hostContext
  path = context.app?.route ?? ""
  applyStyles(context)
  rpc.notify(METHODS.initialized)

  const callTool = async <T>(
    name: string,
    args: Record<string, unknown>
  ): Promise<T> => {
    if (tornDown)
      throw new SdkError({
        code: "unavailable",
        message: "the app was torn down",
      })
    let result: CallToolResult
    try {
      result = await rpc.call<CallToolResult>(METHODS.toolsCall, {
        name,
        arguments: args,
      })
    } catch (err) {
      throw sdkError(err)
    }
    return unwrap<T>(result)
  }

  const listArgs = (q: Query): Record<string, unknown> => ({ ...q })

  const records: Records = {
    list: async (q) =>
      pageOf(await callTool<ListResult>(TOOLS.list, listArgs(q))),
    get: (kind, id) => callTool<SubstrateRecord>(TOOLS.get, { kind, id }),
    put: async (kind, args, opts = {}) => {
      const idempotencyKey = args.id
        ? opts.idempotencyKey
        : (opts.idempotencyKey ?? crypto.randomUUID())
      const params = {
        kind,
        ...args,
        ifVersion: opts.ifVersion,
        idempotencyKey,
      }
      try {
        return await callTool<SubstrateRecord>(TOOLS.put, params)
      } catch (err) {
        if (err instanceof SdkError && err.code === "network" && !args.id) {
          return callTool<SubstrateRecord>(TOOLS.put, params)
        }
        throw err
      }
    },
    patch: (kind, id, args, opts = {}) =>
      callTool<SubstrateRecord>(TOOLS.patch, {
        kind,
        id,
        ...args,
        ifVersion: opts.ifVersion,
      }),
    delete: async (kind, id, opts = {}) => {
      await callTool<unknown>(TOOLS.delete, {
        kind,
        id,
        ifVersion: opts.ifVersion,
      })
    },
    transition: (kind, id, property, to, opts = {}) =>
      callTool<SubstrateRecord>(TOOLS.transition, {
        kind,
        id,
        property,
        to,
        ifVersion: opts.ifVersion,
      }),
    subscribe(q, cb) {
      let id: string | undefined
      let stopped = false
      rpc.call<SubscribeResult>(METHODS.subscribe, listArgs(q)).then(
        (res) => {
          if (stopped) {
            rpc
              .call(METHODS.unsubscribe, { subscription: res.subscription })
              .catch(() => {})
            return
          }
          id = res.subscription
          subscriptions.set(id, cb)
          cb(pageOf(res.page))
        },
        (err: unknown) => {
          if (stopped) return
          cb({ records: [], loading: false, error: sdkError(err) })
        }
      )
      return () => {
        if (stopped) return
        stopped = true
        if (id) {
          subscriptions.delete(id)
          rpc.call(METHODS.unsubscribe, { subscription: id }).catch(() => {})
        }
      }
    },
  }

  const functions: Functions = {
    call: (ref, args, opts = {}) =>
      callTool<FunctionCalledResult>(TOOLS.functionCall, {
        ref,
        args,
        idempotencyKey: opts.idempotencyKey,
      }),
  }

  const agents: Agents = {
    chat(ref, args, onEvent) {
      let stream: string | undefined
      let stopped = false
      callTool<{ stream: string }>(TOOLS.agentChat, {
        ref,
        thread: args.thread,
        message: args.message,
      }).then(
        (res) => {
          if (stopped) {
            callTool(TOOLS.agentStop, { stream: res.stream }).catch(() => {})
            return
          }
          stream = res.stream
          streams.set(stream, (e) => {
            if (e.kind === "done" || e.kind === "error") streams.delete(stream!)
            onEvent(e)
          })
        },
        (err: unknown) => {
          if (!stopped) onEvent({ kind: "error", error: sdkError(err).message })
        }
      )
      return {
        stop() {
          if (stopped) return
          stopped = true
          if (stream) {
            streams.delete(stream)
            callTool(TOOLS.agentStop, { stream }).catch(() => {})
          }
        },
      }
    },
  }

  const setPath = (next: string) => {
    if (next === path) return
    path = next
    routeListeners.fire(path)
  }

  const navigate = async (params: NavigateParams) => {
    try {
      await rpc.call(METHODS.navigate, params)
    } catch (err) {
      throw sdkError(err)
    }
  }

  const host: Host = {
    title: (text) => rpc.notify(METHODS.title, { text }),
    async navigate(target) {
      if ("path" in target && !("app" in target)) {
        setPath(target.path)
        await navigate({ path: target.path })
        return
      }
      if ("record" in target) {
        await navigate({
          record: { kind: target.record.kind, id: target.record.id },
        })
        return
      }
      await navigate({ app: target.app, path: target.path })
    },
    back: () => {
      void navigate({ back: true }).catch(() => {})
    },
    async openLink(url) {
      try {
        await rpc.call(METHODS.openLink, { url })
      } catch (err) {
        throw sdkError(err)
      }
    },
    primaryAction: {
      set: (state) => rpc.notify(METHODS.primaryAction, state),
      onClick: (cb) => primaryListeners.add(cb),
    },
    backButton: {
      onClick: (cb) => backListeners.add(cb),
    },
    async confirm(args) {
      try {
        const res = await rpc.call<{ ok?: boolean }>(METHODS.confirm, args)
        return res?.ok === true
      } catch (err) {
        throw sdkError(err)
      }
    },
    toast: (args) => rpc.notify(METHODS.toast, args),
    notifySizeChanged: () => rpc.notify(METHODS.sizeChanged, measure()),
  }

  const route: Route = {
    get path() {
      return path
    },
    navigate: (next) => {
      void host.navigate({ path: next }).catch(() => {})
    },
    onChange: (cb) => routeListeners.add(cb),
  }

  const declarationFor = (kind: HostContext["kinds"][number]) => {
    let d = declarations.get(kind)
    if (!d) {
      d = declarationOf(kind)
      declarations.set(kind, d)
    }
    return d
  }

  const app: App = {
    get hostContext() {
      return context!
    },
    onHostContextChanged: (cb) => contextListeners.add(cb),
    records,
    functions,
    agents,
    host,
    route,
    kinds: {
      get: (identity) => {
        const kind = context!.kinds?.find((k) => k.identity === identity)
        return kind ? declarationFor(kind) : undefined
      },
      list: () => (context!.kinds ?? []).map(declarationFor),
    },
    inputs: {
      get: (name) => context!.app?.inputs?.[name] ?? { state: "none" },
    },
    subscribe: records.subscribe,
  }

  // In a card the host sizes the frame to the document; report every time
  // the body's box moves, so an app never has to.
  if (typeof ResizeObserver !== "undefined") {
    const observer = new ResizeObserver(() => {
      if (context?.displayMode === "inline") host.notifySizeChanged()
    })
    if (document.body) observer.observe(document.body)
  }

  connected = app
  return app
}

let app: Promise<App> | undefined

/** One app per document: the port has one owner, so a second call shares the
 * first's connection rather than racing it for the port. */
export function createApp(): Promise<App> {
  app ??= connect()
  return app
}

/** The connected app, for code that runs after `createApp()` resolved: the
 * hooks, which render only once the bootstrap awaited it. */
export function currentApp(): App {
  if (!connected) {
    throw new Error(
      "substrate/app: the hooks render only after createApp() resolved"
    )
  }
  return connected
}
