function base64url(bytes: Uint8Array): string {
  let s = ""
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export function randomToken(bytes = 32): string {
  const b = new Uint8Array(bytes)
  crypto.getRandomValues(b)
  return base64url(b)
}

export interface PKCE {
  verifier: string
  challenge: string
}

/** RFC 7636's S256 pair. */
export async function makePKCE(): Promise<PKCE> {
  const verifier = randomToken(48)
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier)
  )
  return { verifier, challenge: base64url(new Uint8Array(digest)) }
}
