/** The image proxy (`GET /api/v1/images?url=`): an applet's screen names images
 * hosted anywhere (a book's cover), and the console draws none of them from
 * another origin. Each is fetched through the server with the session's
 * token and drawn from a blob URL, so the image's host never learns who is
 * looking and the console's img-src stays its own. */

import { API_BASE } from "./http"
import { getToken } from "./session"

/** How many fetched images stay in memory; the oldest URL is revoked. */
const KEPT = 200

const kept = new Map<string, Promise<string | undefined>>()

function keep(url: string, pending: Promise<string | undefined>) {
  kept.set(url, pending)
  while (kept.size > KEPT) {
    const [oldest, gone] = kept.entries().next().value as [
      string,
      Promise<string | undefined>,
    ]
    kept.delete(oldest)
    void gone.then((objectUrl) => objectUrl && URL.revokeObjectURL(objectUrl))
  }
}

/** Whether an image must come through the proxy: an address on another
 * origin. A path on this console and an inline picture are drawn as they
 * are. */
export function proxied(url: string): boolean {
  if (url.startsWith("data:")) return false
  try {
    return new URL(url, window.location.href).origin !== window.location.origin
  } catch {
    return false
  }
}

/** A blob URL of the image at an https address, fetched through the
 * proxy, or undefined when there is no image to draw there. An http
 * address is never fetched: the proxy reaches https alone. */
export function proxiedImage(url: string): Promise<string | undefined> {
  const held = kept.get(url)
  if (held) return held
  const pending = (async () => {
    if (!url.startsWith("https://")) return undefined
    const headers: Record<string, string> = { "X-Substrate-Actor": "console" }
    const token = getToken()
    if (token) headers.Authorization = `Bearer ${token}`
    try {
      const res = await fetch(
        `${API_BASE}/images?url=${encodeURIComponent(url)}`,
        { headers }
      )
      if (!res.ok) return undefined
      return URL.createObjectURL(await res.blob())
    } catch {
      return undefined
    }
  })()
  keep(url, pending)
  return pending
}
