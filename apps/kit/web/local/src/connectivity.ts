/** What the browser says about the network and the page: the outbox sends
 * when the network comes back or the page comes to the front, and the watch
 * reconnects then rather than waiting out its pause. */
export interface Connectivity {
  /** False only when the browser knows it is offline. */
  online(): boolean
  subscribe(fn: (event: "online" | "offline" | "visible") => void): () => void
}

export const browserConnectivity: Connectivity = {
  online: () => typeof navigator === "undefined" || navigator.onLine !== false,
  subscribe(fn) {
    if (typeof window === "undefined") return () => {}
    const online = () => fn("online")
    const offline = () => fn("offline")
    const visible = () => {
      if (document.visibilityState === "visible") fn("visible")
    }
    window.addEventListener("online", online)
    window.addEventListener("offline", offline)
    document.addEventListener("visibilitychange", visible)
    return () => {
      window.removeEventListener("online", online)
      window.removeEventListener("offline", offline)
      document.removeEventListener("visibilitychange", visible)
    }
  },
}

/** A network that is always there and never changes: for tests and for a
 * page outside a browser. */
export const steadyConnectivity: Connectivity = {
  online: () => true,
  subscribe: () => () => {},
}
