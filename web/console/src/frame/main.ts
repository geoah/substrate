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
 * token out of `localStorage`; so a same-origin shell mounts nothing. And it
 * runs only from a document that carries its build (`guest-build.ts`): the
 * import map and the pin come from there, read before `document.open()`
 * erases the element, and kept as the port is kept. */

import {
  MOUNT_TYPE,
  READY_TYPE,
  type MountMessage,
} from "@/lib/apps/bridge/protocol"
import {
  GUEST_BUILD_ID,
  parseGuestBuild,
  pinPolicy,
  type GuestBuild,
} from "./guest-build"
import { mountHtml, mountReact } from "./load"

/** The port outlives the shell's document. */
let port: MessagePort | undefined

const build = parseGuestBuild(
  document.getElementById(GUEST_BUILD_ID)?.textContent
)

/** The second policy, in place before the shell listens for a mount. A meta
 * policy is enforced from the moment it is inserted and can only narrow what
 * the policies before it admit, and the container it lands in survives
 * `document.open()`; what it pins is the shell's own later loads (the
 * transform chunk) and everything the guest's document loads after it. */
function pin(b: GuestBuild): void {
  const meta = document.createElement("meta")
  meta.httpEquiv = "Content-Security-Policy"
  meta.content = pinPolicy(b, location.origin)
  document.head.appendChild(meta)
}

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
  if (e.source !== window.parent || !isMount(e.data) || !build) return
  const [handed] = e.ports
  if (!handed) return
  window.removeEventListener("message", onMessage)
  port = handed
  const mount = e.data
  if (mount.runtime === "html") {
    mountHtml(port, build.imports, mount.source, mount.nonce)
    return
  }
  void mountReact(port, build.imports, {
    source: mount.source,
    modules: mount.modules ?? {},
    nonce: mount.nonce,
  })
}

if (window.origin !== "null" || window.parent === window) {
  document.body.textContent =
    "This page runs only inside the console, in a sandboxed frame."
} else if (!build) {
  document.body.textContent =
    "This page was served without its build (the guest-build block); it mounts nothing."
} else {
  pin(build)
  window.addEventListener("message", onMessage)
  window.parent.postMessage({ type: READY_TYPE }, "*")
}
