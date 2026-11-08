import { describe, expect, it } from "vitest"
import { makePKCE, randomToken } from "./pkce"

describe("PKCE", () => {
  it("makes a verifier in RFC 7636's alphabet and its S256 challenge", async () => {
    const { verifier, challenge } = await makePKCE()
    expect(verifier).toMatch(/^[A-Za-z0-9_-]{43,128}$/)
    const digest = await crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode(verifier)
    )
    const expected = btoa(String.fromCharCode(...new Uint8Array(digest)))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "")
    expect(challenge).toBe(expected)
  })

  it("never repeats a token", () => {
    expect(randomToken()).not.toBe(randomToken())
    expect(randomToken(24)).toHaveLength(32)
  })
})
