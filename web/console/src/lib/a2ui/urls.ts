/** The URLs an applet's screen may reach. Every one is a value an agent wrote
 * or a record holds, so only a web address (or a path on this console)
 * passes: never `javascript:`, a local scheme or a protocol-relative host. */

function webUrl(value: string): string | undefined {
  if (value.startsWith("//")) return undefined
  try {
    const url = new URL(value, window.location.href)
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

// An image is drawn, never run, so a data URL of a picture is as safe as
// the bytes it names. The formats are the image proxy's: never SVG, which
// is a document that can carry its own links and scripts.
const DATA_IMAGE = /^data:image\/(?:png|jpe?g|gif|webp|avif)[;,]/i

/** A source an Image, a Video or an AudioPlayer may load; `data` admits an
 * inline picture. */
export function mediaUrl(
  value: unknown,
  { data = false }: { data?: boolean } = {}
): string | undefined {
  if (typeof value !== "string") return undefined
  const v = value.trim()
  if (!v) return undefined
  if (DATA_IMAGE.test(v)) return data ? v : undefined
  return webUrl(v)
}

/** `openUrl` from a button: a new tab that cannot reach back into the
 * console. False when the URL is not one a button may open. */
export function openUrl(value: unknown): boolean {
  const url = typeof value === "string" ? webUrl(value.trim()) : undefined
  if (!url) return false
  window.open(url, "_blank", "noopener,noreferrer")
  return true
}
