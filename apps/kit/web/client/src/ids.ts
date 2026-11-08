/** A lowercase UUIDv7, chosen on the device so every retry of a create is a
 * PUT that lands the same row (RFC 9562 §5.7). */
export function uuidv7(now = Date.now()): string {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  let ms = BigInt(now)
  for (let i = 5; i >= 0; i--) {
    b[i] = Number(ms & 0xffn)
    ms >>= 8n
  }
  b[6] = (b[6] & 0x0f) | 0x70
  b[8] = (b[8] & 0x3f) | 0x80
  const hex = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
