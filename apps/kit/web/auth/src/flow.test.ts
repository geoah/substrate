import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  SignInError,
  beginSignIn,
  completeSignIn,
  discardStrayCallback,
  isCallback,
} from "./flow"
import { packageDetail, type AppManifest } from "./manifest"

const server = "https://substrate.example.com"
const metadata = {
  issuer: server,
  authorization_endpoint: `${server}/oauth/authorize`,
  token_endpoint: `${server}/oauth/token`,
  revocation_endpoint: `${server}/oauth/revoke`,
  authorization_response_iss_parameter_supported: true,
  authorization_details_types_supported: ["package"],
}
const manifest: AppManifest = {
  id: "template",
  name: "Substrate Template",
  clientID: "https://app.example.com/client.json",
  redirectURI: "https://app.example.com/",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 1)],
  softwareVersion: "1.2.0",
}

/** Begins a sign-in and answers the query the server's redirect carries
 * back, with the state it sent. */
async function begin(repository?: string): Promise<URL> {
  let went = ""
  await beginSignIn({
    manifest,
    server,
    metadata,
    repository,
    navigate: (url) => (went = url),
  })
  return new URL(went)
}

function returnTo(query: Record<string, string>) {
  window.history.replaceState(
    null,
    "",
    "/?" + new URLSearchParams(query).toString()
  )
}

describe("the sign-in", () => {
  beforeEach(() => {
    sessionStorage.clear()
    window.history.replaceState(null, "", "/")
  })
  afterEach(() => vi.unstubAllGlobals())

  it("sends the code flow with PKCE, the details and the build", async () => {
    const url = await begin("ada.example.org")
    expect(url.origin + url.pathname).toBe(metadata.authorization_endpoint)
    const q = url.searchParams
    expect(q.get("response_type")).toBe("code")
    expect(q.get("client_id")).toBe(manifest.clientID)
    expect(q.get("redirect_uri")).toBe(manifest.redirectURI)
    expect(q.get("code_challenge_method")).toBe("S256")
    expect(q.get("code_challenge")).toMatch(/^[A-Za-z0-9_-]{43}$/)
    expect(q.get("login_hint")).toBe("ada.example.org")
    expect(q.get("software_version")).toBe("1.2.0")
    expect(JSON.parse(q.get("authorization_details") ?? "")).toEqual(
      manifest.details
    )
  })

  it("refuses a server that cannot set up the details", async () => {
    await expect(
      beginSignIn({
        manifest,
        server,
        metadata: { ...metadata, authorization_details_types_supported: [] },
        navigate: () => {},
      })
    ).rejects.toMatchObject({ code: "server_too_old" })
  })

  it("refuses a return for another request", async () => {
    await begin()
    returnTo({ code: "c", state: "forged", iss: server })
    expect(isCallback(manifest)).toBe(true)
    await expect(completeSignIn(manifest)).rejects.toMatchObject({
      code: "invalid_state",
    })
    expect(window.location.search).toBe("")
  })

  it("refuses a return from another issuer, or one naming none", async () => {
    const state = (await begin()).searchParams.get("state")!
    returnTo({ code: "c", state, iss: "https://evil.example.net" })
    await expect(completeSignIn(manifest)).rejects.toMatchObject({
      code: "invalid_issuer",
    })
    const again = (await begin()).searchParams.get("state")!
    returnTo({ code: "c", state: again })
    await expect(completeSignIn(manifest)).rejects.toMatchObject({
      code: "invalid_issuer",
    })
  })

  it("says a refusal in the owner's words", async () => {
    const state = (await begin()).searchParams.get("state")!
    returnTo({ error: "access_denied", state, iss: server })
    const error = await completeSignIn(manifest).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(SignInError)
    expect((error as SignInError).message).toBe("Access was not granted.")
  })

  it("refuses a return when no sign-in began here", async () => {
    returnTo({ code: "c", state: "s" })
    expect(isCallback(manifest)).toBe(false)
    await expect(completeSignIn(manifest)).rejects.toMatchObject({
      code: "invalid_state",
    })
  })

  it("exchanges the code with its verifier for a session", async () => {
    const url = await begin()
    const challenge = url.searchParams.get("code_challenge")!
    returnTo({
      code: "the-code",
      state: url.searchParams.get("state")!,
      iss: server,
    })
    const fetch = vi.fn(async (_: string, init: RequestInit) => {
      const body = new URLSearchParams(init.body as URLSearchParams)
      const digest = await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(body.get("code_verifier")!)
      )
      const derived = btoa(String.fromCharCode(...new Uint8Array(digest)))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "")
      expect(derived).toBe(challenge)
      expect(body.get("code")).toBe("the-code")
      expect(body.get("redirect_uri")).toBe(manifest.redirectURI)
      return new Response(
        JSON.stringify({
          access_token: "tok",
          repository: "ada.example.org",
          scope: "write:substrate.reamde.dev/template/*",
          expires_in: 3600,
          token_id: "t1",
        })
      )
    })
    vi.stubGlobal("fetch", fetch)
    const session = await completeSignIn(manifest)
    expect(session).toMatchObject({
      server,
      repository: "ada.example.org",
      token: "tok",
      tokenID: "t1",
      scopes: ["write:substrate.reamde.dev/template/*"],
      requestedScopes: manifest.scopes,
      revocationEndpoint: metadata.revocation_endpoint,
    })
    expect(session.expiresAt).toBeGreaterThan(Date.now())
  })

  it("revokes and refuses a token for another repository than the one typed", async () => {
    const url = await begin("ada.example.org")
    returnTo({ code: "c", state: url.searchParams.get("state")!, iss: server })
    const calls: string[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: string) => {
        calls.push(String(input))
        return Promise.resolve(
          String(input).endsWith("/token")
            ? new Response(
                JSON.stringify({
                  access_token: "tok",
                  repository: "bob.example.org",
                })
              )
            : new Response("{}")
        )
      })
    )
    await expect(completeSignIn(manifest)).rejects.toMatchObject({
      code: "repository_mismatch",
    })
    expect(calls).toEqual([
      metadata.token_endpoint,
      metadata.revocation_endpoint,
    ])
  })

  it("takes a code no sign-in here asked for out of the address bar", () => {
    window.history.replaceState(
      null,
      "",
      "/?code=c&state=s&iss=x&keep=1#/item/a"
    )
    expect(isCallback(manifest)).toBe(false)
    expect(discardStrayCallback(manifest)).toBe(true)
    expect(window.location.search).toBe("?keep=1")
    expect(window.location.hash).toBe("#/item/a")
    expect(discardStrayCallback(manifest)).toBe(false)
  })

  it("leaves a return this tab began for the exchange", async () => {
    const url = await begin()
    returnTo({ code: "c", state: url.searchParams.get("state")! })
    expect(discardStrayCallback(manifest)).toBe(false)
    expect(new URLSearchParams(window.location.search).get("code")).toBe("c")
  })
})
