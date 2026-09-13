/** `substrate/app`: what an app's source imports. It is built as its own
 * entry chunk so the guest shell's import map can name it; in dev the map
 * points at `/src/apps-sdk/index.ts` and vite transforms it on request, in a
 * build at the content-hashed `/assets/app-sdk-<hash>.js` the build writes
 * into `app-frame.html`'s guest-build block, because the shell cannot read
 * a manifest from an origin whose `connect-src` is `'none'`.
 *
 * Everything on `App` is also a module-level export (`records`, `functions`,
 * `agents`, `host`) that connects lazily, so `import { records } from
 * "substrate/app"` at the top of a component file works as the examples do.
 * `__boot` is the shell's entry point and not part of the contract. */

import { Component, type ErrorInfo, type ReactNode } from "react"

import {
  createApp,
  locate,
  reportError,
  type Agents,
  type App,
  type Functions,
  type Host,
  type Records,
  type Unsubscribe,
} from "./core"

export * from "./core"
export * from "./hooks"

function later<T>(call: (app: App) => Promise<T> | T): Promise<T> {
  return createApp().then(call)
}

/** A subscription or a listener made before the bridge is up: the
 * unsubscribe is answered now and honoured once the connection lands. */
function deferred(attach: (app: App) => Unsubscribe): Unsubscribe {
  let off: Unsubscribe | undefined
  let stopped = false
  void createApp().then((app) => {
    if (stopped) return
    off = attach(app)
  })
  return () => {
    stopped = true
    off?.()
  }
}

export const records: Records = {
  list: (q) => later((a) => a.records.list(q)),
  get: (kind, id) => later((a) => a.records.get(kind, id)),
  put: (kind, args, opts) => later((a) => a.records.put(kind, args, opts)),
  patch: (kind, id, args, opts) =>
    later((a) => a.records.patch(kind, id, args, opts)),
  delete: (kind, id, opts) => later((a) => a.records.delete(kind, id, opts)),
  transition: (kind, id, property, to, opts) =>
    later((a) => a.records.transition(kind, id, property, to, opts)),
  subscribe: (q, cb) => deferred((a) => a.records.subscribe(q, cb)),
}

export const functions: Functions = {
  call: (ref, args, opts) => later((a) => a.functions.call(ref, args, opts)),
}

export const agents: Agents = {
  chat(ref, args, onEvent) {
    let handle: { stop(): void } | undefined
    let stopped = false
    void createApp().then((a) => {
      if (stopped) return
      handle = a.agents.chat(ref, args, onEvent)
    })
    return {
      stop() {
        stopped = true
        handle?.stop()
      },
    }
  },
}

export const host: Host = {
  title: (text) => void later((a) => a.host.title(text)),
  navigate: (target) => later((a) => a.host.navigate(target)),
  back: () => void later((a) => a.host.back()),
  openLink: (url) => later((a) => a.host.openLink(url)),
  primaryAction: {
    set: (state) => void later((a) => a.host.primaryAction.set(state)),
    onClick: (cb) => deferred((a) => a.host.primaryAction.onClick(cb)),
  },
  backButton: {
    onClick: (cb) => deferred((a) => a.host.backButton.onClick(cb)),
  },
  confirm: (args) => later((a) => a.host.confirm(args)),
  toast: (args) => void later((a) => a.host.toast(args)),
  notifySizeChanged: () => void later((a) => a.host.notifySizeChanged()),
}

/** The kit's own frames, elided from a component stack before it reaches
 * the strip: the author's line is what helps, not the kit's. */
const KIT_FRAME = /apps-sdk\/ui\/|\/assets\/app-ui\.js/

interface BoundaryState {
  error?: Error
}

/** Every rendered app sits under this: a render error reaches the host as a
 * runtime error with the author's line, and the rectangle goes quiet
 * instead of half-drawn. */
export class ErrorBoundary extends Component<
  { children: ReactNode },
  BoundaryState
> {
  state: BoundaryState = {}

  static getDerivedStateFromError(error: Error): BoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    const componentStack = (info.componentStack ?? "")
      .split("\n")
      .filter((line) => line.trim() && !KIT_FRAME.test(line))
      .join("\n")
    reportError({
      phase: "runtime",
      message: error.message,
      stack: [error.stack ?? "", componentStack].filter(Boolean).join("\n"),
      ...locate(error.stack),
    })
  }

  render(): ReactNode {
    return this.state.error ? null : this.props.children
  }
}

type CreateRoot = (el: Element) => { render(node: unknown): void }
type Jsx = (type: unknown, props: Record<string, unknown>) => unknown

/** What the shell's inline bootstrap calls with the entry module: render its
 * default export into `#root` under the boundary, or hand `#root` and the
 * app to its `mount`, or say that it exports neither. `createRoot` and `jsx`
 * arrive from the shell so this module does not import `react-dom/client`. */
export async function __boot(
  mod: Record<string, unknown>,
  createRoot: CreateRoot,
  jsx: Jsx
): Promise<void> {
  const app = await createApp()
  const root = document.getElementById("root")
  if (!root) {
    reportError({ phase: "runtime", message: "the document has no #root" })
    return
  }
  if (typeof mod.default === "function") {
    createRoot(root).render(
      jsx(ErrorBoundary, { children: jsx(mod.default, {}) })
    )
    return
  }
  if (typeof mod.mount === "function") {
    await (mod.mount as (el: Element, app: App) => unknown)(root, app)
    return
  }
  reportError({
    phase: "runtime",
    module: "source",
    message:
      "the module exports neither a default component nor mount(root, app)",
  })
}
