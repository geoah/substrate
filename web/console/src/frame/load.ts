/** The two arms of the shell. `html` writes the source as the document, the
 * import map prepended. `react` transforms the source and every module,
 * mints a `blob:` URL per module, writes the import map with the `#name`
 * entries added, imports the entry and renders it.
 *
 * The import map's five specifiers come from the build (`guest-build.ts`);
 * the shell hands them in, read before its document was replaced.
 *
 * Errors reach the host over the port as `substrate/notifications/error`: a
 * transform error before anything runs, a runtime error from `window`'s own
 * listeners, whose `filename` is the blob URL the module was minted at and
 * whose line is the author's line because the transform preserves them. The
 * port is written to directly; the SDK owns its receiving end once it
 * loads. */

import {
  LEFT_TYPE,
  METHODS,
  notification,
  type LeftMessage,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  type GuestError,
} from "@/lib/apps/bridge/protocol"
import { SOURCE_MODULE } from "@/lib/apps/spec"

const VIEWPORT =
  '<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover, interactive-widget=resizes-content">'

export function importMapScript(imports: Record<string, string>): string {
  return `<script type="importmap">${JSON.stringify({ imports })}</script>`
}

/** The doctype leads, because a doctype that is not the first token is
 * ignored and the document would parse in quirks mode, where the body
 * reports the viewport as its height and a card never shrinks to fit. */
export function prelude(imports: Record<string, string>): string {
  return "<!doctype html>" + importMapScript(imports) + VIEWPORT
}

function post(port: MessagePort, error: GuestError): void {
  port.postMessage(notification(METHODS.error, error))
}

/** The host, captured before any document the guest writes can replace
 * `window.parent`. */
const host = window.parent

/** Registered after `document.open()`, which erases the window's listeners,
 * and before the guest's first script: `pagehide` is the one event every way
 * out of this document fires, the page a blocked cross-origin target leaves
 * and a same-origin target alike, and the host closes the view on hearing it
 * instead of waiting for the pings to stop. The guest holds no reference to
 * the listener and cannot redefine a cross-origin window's `postMessage`. */
export function announceLeave(nonce: string): void {
  window.addEventListener("pagehide", () =>
    host.postMessage({ type: LEFT_TYPE, nonce } satisfies LeftMessage, "*")
  )
}

/** Registered AFTER `document.open()`, which is what lets it survive: the
 * SDK asks for the port whenever its module runs, and the answer is
 * synchronous. */
export function servePort(port: MessagePort): void {
  window.addEventListener(PORT_REQUEST_EVENT, () => {
    window.dispatchEvent(new CustomEvent(PORT_EVENT, { detail: port }))
  })
}

/** The first frame of a stack that names one of the app's own modules. */
export function locate(
  names: Map<string, string>,
  stack: string | undefined
): Pick<GuestError, "module" | "line" | "column"> {
  if (!stack) return {}
  const m = stack.match(/(blob:[^\s):]+(?::[^\s):]+)?):(\d+):(\d+)/)
  if (!m) return {}
  return {
    module: names.get(m[1]),
    line: Number(m[2]),
    column: Number(m[3]),
  }
}

/** Runtime errors, forwarded with the author's module and line. The SDK's
 * boundary reports render errors itself; these catch what runs outside a
 * render: module evaluation, handlers, rejected promises. */
export function forwardErrors(
  port: MessagePort,
  names: Map<string, string>
): void {
  window.addEventListener("error", (e) => {
    const located = locate(names, e.error?.stack)
    post(port, {
      phase: "runtime",
      message: e.message,
      module: (e.filename && names.get(e.filename)) || located.module,
      line: e.lineno || located.line,
      column: e.colno || located.column,
      stack: e.error?.stack,
    })
  })
  window.addEventListener("unhandledrejection", (e) => {
    const reason = e.reason as Error | undefined
    post(port, {
      phase: "runtime",
      message: reason?.message ?? String(e.reason),
      stack: reason?.stack,
      ...locate(names, reason?.stack),
    })
  })
}

export interface ReactMount {
  source: string
  modules: Record<string, string>
  /** The mount's nonce, echoed when the document is unloaded. */
  nonce: string
}

/** The bootstrap the shell writes after the import map: inline is admitted
 * by the policy, and the map precedes it in the same write, so it is in the
 * document before any specifier resolves. The entry is imported by its own
 * URL, never through the map. */
export function bootstrapScript(entry: string): string {
  return (
    '<script type="module">' +
    'import { createRoot } from "react-dom/client";' +
    'import { jsx } from "react/jsx-runtime";' +
    'import { __boot } from "substrate/app";' +
    `const mod = await import(${JSON.stringify(entry)});` +
    "await __boot(mod, createRoot, jsx);" +
    "</script>"
  )
}

export async function mountReact(
  port: MessagePort,
  imports: Record<string, string>,
  mount: ReactMount
): Promise<void> {
  const { transformModule } = await import("./transform")
  const names = new Map<string, string>()
  const mint = (name: string, text: string): string | undefined => {
    const out = transformModule(name, text)
    if ("error" in out) {
      post(port, { phase: "transform", ...out.error })
      return undefined
    }
    const url = URL.createObjectURL(
      new Blob([out.js], { type: "text/javascript" })
    )
    names.set(url, name)
    return url
  }
  // The entry is minted first and held apart from the authored modules: it
  // is never a key of the map, so no `modules` name can take its place, and
  // a module spelled as the entry is refused here as the decoder refuses it,
  // because two modules under one name would share every error's attribution.
  const entry = mint(SOURCE_MODULE, mount.source)
  if (!entry) return
  const map: Record<string, string> = { ...imports }
  for (const [name, text] of Object.entries(mount.modules)) {
    if (name === SOURCE_MODULE) {
      post(port, {
        phase: "transform",
        module: name,
        message: `${name} is the entry's name; a module has its own`,
      })
      return
    }
    const url = mint(name, text)
    if (!url) return
    map[`#${name}`] = url
  }

  document.open()
  announceLeave(mount.nonce)
  document.write(
    prelude(map) +
      "<style>html,body,#root{height:100%;margin:0}</style>" +
      '<div id="root"></div>' +
      bootstrapScript(entry)
  )
  document.close()
  servePort(port)
  forwardErrors(port, names)
}

export function mountHtml(
  port: MessagePort,
  imports: Record<string, string>,
  html: string,
  nonce: string
): void {
  document.open()
  announceLeave(nonce)
  document.write(prelude(imports) + html)
  document.close()
  servePort(port)
  forwardErrors(port, new Map())
}
