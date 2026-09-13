/** The two arms of the shell. `html` writes the source as the document, the
 * import map prepended. `react` transforms the source and every module,
 * mints a `blob:` URL per module, writes the import map with the `#name`
 * entries added, imports the entry and renders it.
 *
 * Errors reach the host over the port as `substrate/notifications/error`: a
 * transform error before anything runs, a runtime error from `window`'s own
 * listeners, whose `filename` is the blob URL the module was minted at and
 * whose line is the author's line because the transform preserves them. The
 * port is written to directly; the SDK owns its receiving end once it
 * loads. */

import {
  METHODS,
  notification,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  type GuestError,
} from "@/lib/apps/bridge/protocol"

/** `substrate/app`, `substrate/ui`, `react` and its two siblings, resolved
 * by the import map. Dev serves the module sources transformed on request; a
 * build names the chunks without a hash so these strings hold without a
 * manifest (vite.config.ts, `entryFileNames`). */
export const SPEC: Record<string, string> = import.meta.env.DEV
  ? {
      react: "/src/apps-sdk/react.ts",
      "react/jsx-runtime": "/src/apps-sdk/jsx-runtime.ts",
      "react-dom/client": "/src/apps-sdk/react-dom-client.ts",
      "substrate/app": "/src/apps-sdk/index.ts",
      "substrate/ui": "/src/apps-sdk/ui/index.ts",
    }
  : {
      react: "/assets/app-react.js",
      "react/jsx-runtime": "/assets/app-jsx-runtime.js",
      "react-dom/client": "/assets/app-react-dom-client.js",
      "substrate/app": "/assets/app-sdk.js",
      "substrate/ui": "/assets/app-ui.js",
    }

/** The name the entry is minted under in the import map (`#source`), so a
 * runtime error's blob URL reads back as `source` like the others. */
export const SOURCE_NAME = "source"

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
}

/** The bootstrap the shell writes after the import map: inline is admitted
 * by the policy, and the map precedes it in the same write, so it is in the
 * document before any specifier resolves. */
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
  mount: ReactMount
): Promise<void> {
  const { transformModule } = await import("./transform")
  const texts: [string, string][] = [
    [SOURCE_NAME, mount.source],
    ...Object.entries(mount.modules),
  ]
  const urls = new Map<string, string>()
  const names = new Map<string, string>()
  for (const [name, text] of texts) {
    const out = transformModule(name, text)
    if ("error" in out) {
      post(port, { phase: "transform", ...out.error })
      return
    }
    const url = URL.createObjectURL(
      new Blob([out.js], { type: "text/javascript" })
    )
    urls.set(name, url)
    names.set(url, name)
  }
  const imports: Record<string, string> = { ...SPEC }
  for (const [name, url] of urls) imports[`#${name}`] = url
  const entry = urls.get(SOURCE_NAME)!

  document.open()
  document.write(
    prelude(imports) +
      "<style>html,body,#root{height:100%;margin:0}</style>" +
      '<div id="root"></div>' +
      bootstrapScript(entry)
  )
  document.close()
  servePort(port)
  forwardErrors(port, names)
}

export function mountHtml(port: MessagePort, html: string): void {
  document.open()
  document.write(prelude(SPEC) + html)
  document.close()
  servePort(port)
  forwardErrors(port, new Map())
}
