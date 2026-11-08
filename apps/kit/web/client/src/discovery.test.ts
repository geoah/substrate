import { afterEach, describe, expect, it, vi } from "vitest"
import {
  NotASubstrate,
  ServerTooOld,
  locate,
  originOf,
  parseSubstrateTXT,
  probe,
} from "./discovery"

describe("parseSubstrateTXT", () => {
  it("reads the server a repository's record names", () => {
    expect(
      parseSubstrateTXT('"v=substrate1 server=https://substrate.example.com"')
    ).toBe("https://substrate.example.com")
  })

  it("joins a record split into several strings", () => {
    expect(
      parseSubstrateTXT(
        '"v=substrate1 server=https://subst" "rate.example.com"'
      )
    ).toBe("https://substrate.example.com")
  })

  it("completes a bare host with https", () => {
    expect(parseSubstrateTXT("v=substrate1 server=example.com")).toBe(
      "https://example.com"
    )
  })

  it("refuses another record, one with no server, and a cleartext server", () => {
    expect(parseSubstrateTXT('"v=spf1 include:example.com"')).toBeUndefined()
    expect(
      parseSubstrateTXT('"v=substrate1 doc=https://example.com/a.json"')
    ).toBeUndefined()
    expect(
      parseSubstrateTXT('"v=substrate1 server=http://example.com"')
    ).toBeUndefined()
  })
})

describe("originOf", () => {
  it("allows cleartext only to a loopback address", () => {
    expect(originOf("http://localhost:8080/x")).toBe("http://localhost:8080")
    expect(originOf("http://example.com")).toBeUndefined()
    expect(originOf("ada.example.com")).toBe("https://ada.example.com")
    expect(originOf("intranet")).toBeUndefined()
    expect(originOf("  ")).toBeUndefined()
  })
})

const origin = "https://substrate.example.com"
const metadata = {
  issuer: origin,
  authorization_endpoint: `${origin}/oauth/authorize`,
  token_endpoint: `${origin}/oauth/token`,
  scopes_supported: ["read:*", "write:*"],
  authorization_details_types_supported: ["package"],
}
const document = {
  server: { version: "v0.121.0" },
  features: [{ name: "apps", stability: "alpha" }],
}

function serve(routes: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: string) => {
      const url = String(input)
      for (const [path, body] of Object.entries(routes))
        if (url.endsWith(path))
          return Promise.resolve(new Response(JSON.stringify(body)))
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
  )
}

describe("probe", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("answers a substrate that meets the requirement", async () => {
    serve({
      "/.well-known/oauth-authorization-server": metadata,
      "/.well-known/substrate/server.json": document,
    })
    const found = await probe(origin, undefined, {
      scopeVerbs: ["write"],
      detailTypes: ["package"],
    })
    expect(found.server).toBe(origin)
  })

  it("says what a server too old cannot do", async () => {
    serve({
      "/.well-known/oauth-authorization-server": metadata,
      "/.well-known/substrate/server.json": {
        ...document,
        server: { version: "v0.100.0" },
      },
    })
    const error = await probe(origin, undefined, {
      minimumVersion: "0.113.0",
      scopeVerbs: ["agents"],
      detailTypes: ["package", "webhook"],
    }).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ServerTooOld)
    const verdict = (error as ServerTooOld).verdict
    expect(verdict.needed).toBe("0.113.0")
    expect(verdict.missing).toEqual([
      "let apps talk to your agents",
      "turn on an app's webhook when you approve it",
    ])
  })

  it("refuses metadata that names its sign-in on another server", async () => {
    serve({
      "/.well-known/oauth-authorization-server": {
        ...metadata,
        token_endpoint: "https://elsewhere.example.net/token",
      },
      "/.well-known/substrate/server.json": document,
    })
    await expect(probe(origin)).rejects.toBeInstanceOf(NotASubstrate)
  })

  it("refuses what is no substrate", async () => {
    serve({})
    await expect(probe(origin)).rejects.toThrow(/is not a Substrate server/)
  })
})

describe("locate", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("finds a repository's server through its TXT record", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: string) => {
        const url = String(input)
        if (url.startsWith("https://cloudflare-dns.com/"))
          return Promise.resolve(
            new Response(
              JSON.stringify({
                Answer: [{ type: 16, data: `"v=substrate1 server=${origin}"` }],
              })
            )
          )
        if (url === `${origin}/.well-known/oauth-authorization-server`)
          return Promise.resolve(new Response(JSON.stringify(metadata)))
        if (url === `${origin}/.well-known/substrate/server.json`)
          return Promise.resolve(new Response(JSON.stringify(document)))
        return Promise.resolve(new Response("{}", { status: 404 }))
      })
    )
    const found = await locate("ada.example.org")
    expect(found).toMatchObject({
      server: origin,
      repository: "ada.example.org",
    })
  })
})
