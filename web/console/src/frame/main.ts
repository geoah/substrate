/** The guest shell: what `/app-frame.html` runs before a view's document
 * replaces it. It exists so the document can carry the host's policy (the
 * meta CSP in the HTML) without inheriting the console's, which a `srcdoc`
 * frame would; and so the document is written by code that already holds the
 * port, because `document.open()` erases every listener on the window and
 * the document but not a module's own variables.
 *
 * The shell runs ONLY on an opaque origin. Framed without `sandbox`, it
 * would be the console's origin, and a document it mounted could read the
 * token out of `localStorage`; so a same-origin shell mounts nothing. */

import {
  MOUNT_TYPE,
  PORT_EVENT,
  PORT_REQUEST_EVENT,
  READY_TYPE,
  type MountMessage,
} from "@/lib/apps/bridge/protocol"

/** Where the import map sends `substrate/app`. Dev serves the module source
 * transformed on request; a build names the SDK chunk without a hash so this
 * string holds without a manifest (vite.config.ts, `entryFileNames`). */
const SDK_URL = import.meta.env.DEV
  ? "/src/apps-sdk/index.ts"
  : "/assets/app-sdk.js"

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
  // Registered AFTER open(), which is what lets it survive: the SDK asks for
  // the port whenever its module runs, and the answer is synchronous.
  window.addEventListener(PORT_REQUEST_EVENT, () => {
    window.dispatchEvent(new CustomEvent(PORT_EVENT, { detail: port }))
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
