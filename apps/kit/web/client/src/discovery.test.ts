import { afterEach, describe, expect, it, vi } from "vitest"
import {
  NotASubstrate,
  ServerTooOld,
  hasFeature,
  locate,
  originOf,
  parseSubstrateTXT,
  probe,
  type ServerDocument,
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
  features: [{ name: "apps", stability: "beta", revision: 1 }],
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

describe("hasFeature", () => {
  const served: ServerDocument = {
    features: [
      { name: "records", stability: "stable", revision: 3 },
      {
        name: "auth",
        stability: "beta",
        revision: 1,
        notice:
          "Before 1.0, sign-in names a person, who then picks a repository.",
      },
      { name: "apps", stability: "beta" },
    ],
  }

  it("answers a feature the server lists, and no other", () => {
    expect(hasFeature(served, "records")).toBe(true)
    expect(hasFeature(served, "merge")).toBe(false)
    expect(hasFeature(undefined, "records")).toBe(false)
    expect(hasFeature({}, "records")).toBe(false)
  })

  it("holds a feature to the revision asked for", () => {
    expect(hasFeature(served, "records", 2)).toBe(true)
    expect(hasFeature(served, "records", 3)).toBe(true)
    expect(hasFeature(served, "records", 4)).toBe(false)
    expect(hasFeature(served, "merge", 0)).toBe(false)
  })

  it("reads a server from before revisions as meeting no minimum", () => {
    expect(hasFeature(served, "apps")).toBe(true)
    expect(hasFeature(served, "apps", 1)).toBe(false)
    const odd = {
      features: [{ name: "search", stability: "beta", revision: "2" }],
    } as unknown as ServerDocument
    expect(hasFeature(odd, "search")).toBe(true)
    expect(hasFeature(odd, "search", 1)).toBe(false)
    const negative = {
      features: [{ name: "apps", stability: "beta", revision: -1 }],
    } as unknown as ServerDocument
    expect(hasFeature(negative, "apps")).toBe(true)
    expect(
      hasFeature({ features: "x" } as unknown as ServerDocument, "search")
    ).toBe(false)
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
