/** The guest shell: what `/app-frame.html` runs before an app's document
 * replaces it. It exists so the document can carry the host's policy (the
 * meta CSP in the HTML) without inheriting the console's, which a `srcdoc`
 * frame would; and so the document is written by code that already holds the
 * port, because `document.open()` erases every listener on the window and
 * the document but not a module's own variables.
 *
 * Two arms (`load.ts`). `html` writes the source as the document, the import
 * map prepended. `react` transforms the source and the modules, mints a
 * `blob:` URL per module, and renders the entry.
 *
 * The shell runs ONLY on an opaque origin. Framed without `sandbox`, it
 * would be the console's origin, and a document it mounted could read the
 * token out of `localStorage`; so a same-origin shell mounts nothing. */

import {
  MOUNT_TYPE,
  READY_TYPE,
  type MountMessage,
} from "@/lib/apps/bridge/protocol"
import { mountHtml, mountReact } from "./load"

/** The port outlives the shell's document. */
let port: MessagePort | undefined

function isMount(data: unknown): data is MountMessage {
  const m = data as Partial<MountMessage> | null
  return (
    Boolean(m) &&
    m!.type === MOUNT_TYPE &&
    typeof m!.source === "string" &&
    (m!.runtime === "react" || m!.runtime === "html")
  )
}

function onMessage(e: MessageEvent): void {
  if (e.source !== window.parent || !isMount(e.data)) return
  const [handed] = e.ports
  if (!handed) return
  window.removeEventListener("message", onMessage)
  port = handed
  const mount = e.data
  if (mount.runtime === "html") {
    mountHtml(port, mount.source)
    return
  }
  void mountReact(port, {
    source: mount.source,
    modules: mount.modules ?? {},
  })
}

if (window.origin !== "null" || window.parent === window) {
  document.body.textContent =
    "This page runs only inside the console, in a sandboxed frame."
} else {
  window.addEventListener("message", onMessage)
  window.parent.postMessage({ type: READY_TYPE }, "*")
}
