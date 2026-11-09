// The ids an import writes. An app may create a record of a kind with
// minted ids only under an id in the minted shape (decision 0228), so every
// id the plan names is derived as the server derives one
// (vocabulary.DerivedMintedID): the first 16 bytes of the sha256 of its
// parts, joined by NUL, marked as a UUIDv8 and written as 26 Crockford
// characters. The plan's keys stay the parts, so the import record alone
// still says what it writes (docs/plans/healthspan.md, The package, import).

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
  0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
  0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
  0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

/** SHA-256 of `bytes`, synchronous: the plan names its ids while it is
 * built, and `crypto.subtle` answers only in a promise and only in a
 * secure context. */
export function sha256(bytes: Uint8Array): Uint8Array {
  const h = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c,
    0x1f83d9ab, 0x5be0cd19,
  ])
  const bitLength = bytes.length * 8
  const padded = new Uint8Array(Math.ceil((bytes.length + 9) / 64) * 64)
  padded.set(bytes)
  padded[bytes.length] = 0x80
  const view = new DataView(padded.buffer)
  view.setUint32(padded.length - 8, Math.floor(bitLength / 2 ** 32))
  view.setUint32(padded.length - 4, bitLength >>> 0)
  const w = new Uint32Array(64)
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(off + i * 4)
    for (let i = 16; i < 64; i++) {
      const a = w[i - 15]
      const b = w[i - 2]
      const s0 = ((a >>> 7) | (a << 25)) ^ ((a >>> 18) | (a << 14)) ^ (a >>> 3)
      const s1 =
        ((b >>> 17) | (b << 15)) ^ ((b >>> 19) | (b << 13)) ^ (b >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0
    }
    let [a, b, c, d, e, f, g, hh] = h
    for (let i = 0; i < 64; i++) {
      const S1 =
        ((e >>> 6) | (e << 26)) ^
        ((e >>> 11) | (e << 21)) ^
        ((e >>> 25) | (e << 7))
      const ch = (e & f) ^ (~e & g)
      const t1 = (hh + S1 + ch + K[i] + w[i]) >>> 0
      const S0 =
        ((a >>> 2) | (a << 30)) ^
        ((a >>> 13) | (a << 19)) ^
        ((a >>> 22) | (a << 10))
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) >>> 0
      hh = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }
    h[0] += a
    h[1] += b
    h[2] += c
    h[3] += d
    h[4] += e
    h[5] += f
    h[6] += g
    h[7] += hh
  }
  const out = new Uint8Array(32)
  const outView = new DataView(out.buffer)
  for (let i = 0; i < 8; i++) outView.setUint32(i * 4, h[i])
  return out
}

const CROCKFORD = "0123456789abcdefghjkmnpqrstvwxyz"

/** A UUID's 16 bytes as the 26 characters of a minted id, most significant
 * first, the first character carrying two zero bits. */
export function encodeMinted(b: Uint8Array): string {
  let out = ""
  for (let i = 0; i < 26; i++) {
    let v = 0
    for (let k = i * 5 - 2; k < i * 5 + 3; k++) {
      v <<= 1
      if (k >= 0) v |= (b[k >> 3] >> (7 - (k % 8))) & 1
    }
    out += CROCKFORD[v]
  }
  return out
}

/** An id in the minted shape derived from `parts`, the server's
 * DerivedMintedID: the same parts always name the same record. */
export function derivedMintedId(...parts: string[]): string {
  const sum = sha256(new TextEncoder().encode(parts.join("\x00")))
  const u = sum.slice(0, 16)
  u[6] = (u[6] & 0x0f) | 0x80
  u[8] = (u[8] & 0x3f) | 0x80
  return encodeMinted(u)
}

/** The id of the report the plan keys `key`: `<import id>/report/<key>`'s
 * parts. */
export function reportId(importId: string, key: string): string {
  return derivedMintedId(importId, "report", key)
}

/** The id of the result the plan keys `key`: `<import id>/<key>`'s parts. */
export function resultId(importId: string, key: string): string {
  return derivedMintedId(importId, key)
}

/** A lab's name as the readable part of its source's id: lowercased, its
 * runs of anything but letters and digits one hyphen. */
export function labSlug(name: string): string {
  const slug = name
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
  return slug || "lab"
}

/** The source id an earlier writer that names its records gave a lab
 * (`lab/<slug>`), which setup takes where it is a lab. */
export function readableLabId(name: string): string {
  return `lab/${labSlug(name)}`
}

/** The source id setup creates a lab's source under: `lab/<slug>`'s parts,
 * and a number past the first where another record holds the id. */
export function labSourceId(name: string, attempt = 1): string {
  const slug = labSlug(name)
  return attempt <= 1
    ? derivedMintedId("lab", slug)
    : derivedMintedId("lab", `${slug}-${attempt}`)
}
