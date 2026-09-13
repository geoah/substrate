/** The guest shell: what `/app-frame.html` runs before a view's document
 * replaces it. It exists so the document can carry the host's policy (the
 * meta CSP in the HTML) without inheriting the console's, which a `srcdoc`
 * frame would; and so the document is written by code that already holds the
 * port, because `document.open()` erases every listener on the window and
 * the document but not a module's own variables.
 *
 * The shell runs ONLY on an opaque origin. Framed without `sandbox`, it
 * would be the console's origin, and a document it mounted could read the
 * token out of `localStorage`; so a same-origin shell mounts nothing.
 *
 * The shell's module and the SDK are fetched cross-origin from that opaque
 * origin, so they load only where `/src` and `/assets` answer CORS: vite dev
 * does (vite.config.ts), the Go server's static handler does not yet, so a
 * built console mounts no custom view until it does (docs/plans/apps.md). */

import sdkUrl from "virtual:substrate-sdk-url"

import {
  METHODS,
  MOUNT_TYPE,
  notification,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  READY_TYPE,
  type MountMessage,
} from "@/lib/apps/bridge/protocol"

/** Where the import map sends `substrate/app`: the SDK's source under
 * `vite dev`, transformed on request, and its CONTENT-HASHED chunk in a
 * build, resolved by the bundler (vite.config.ts, `sdkUrl`) so the name is
 * never a string this file guesses. */
const SDK_URL: string = sdkUrl

/** The port outlives the shell's document. */
let port: MessagePort | undefined

function isMount(data: unknown): data is MountMessage {
  const m = data as Partial<MountMessage> | null
  return Boolean(m) && m!.type === MOUNT_TYPE && typeof m!.html === "string"
}

function mount(html: string): void {
  const importMap = JSON.stringify({ imports: { "substrate/app": SDK_URL } })
  // The doctype leads, because a doctype that is not the first token is
  // ignored and the document would parse in quirks mode, where the body
  // reports the viewport as its height and a card never shrinks to fit.
  const prelude =
    "<!doctype html>" +
    `<script type="importmap">${importMap}</script>` +
    `<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover, interactive-widget=resizes-content">`
  document.open()
  document.write(prelude + html)
  document.close()
  // Registered AFTER open(), which is what lets them survive. The SDK asks
  // for the port whenever its module runs, and the answer is synchronous.
  window.addEventListener(PORT_REQUEST_EVENT, () => {
    window.dispatchEvent(new CustomEvent(PORT_EVENT, { detail: port }))
  })
  // The document leaving its frame: navigated by its own script, a link, a
  // form or a meta refresh. The console's `frame-src 'self'` admits no
  // off-origin destination, so nothing is fetched and the browser commits
  // its own error page instead, which would otherwise sit there until the
  // host's pings gave up. Said on the port the shell keeps, from a handler
  // no guest code holds a reference to, so the host tears down at once.
  window.addEventListener("pagehide", () => {
    port?.postMessage(notification(METHODS.unload))
  })
}

function onMessage(e: MessageEvent): void {
  if (e.source !== window.parent || !isMount(e.data)) return
  const [handed] = e.ports
  if (!handed) return
  window.removeEventListener("message", onMessage)
  port = handed
  mount(e.data.html)
}

if (window.origin !== "null" || window.parent === window) {
  document.body.textContent =
    "This page runs only inside the console, in a sandboxed frame."
} else {
  window.addEventListener("message", onMessage)
  window.parent.postMessage({ type: READY_TYPE }, "*")
}
