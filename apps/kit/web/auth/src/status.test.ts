import { FakeSubstrate } from "@substrate/testing"
import { afterEach, describe, expect, it, vi } from "vitest"
import { packageDetail, type AppManifest } from "./manifest"
import type { Session } from "./session"
import { checkAppStatus, endingWindow } from "./status"

const manifest: AppManifest = {
  id: "template",
  name: "Substrate Template",
  clientID: "https://app.example.com/client.json",
  redirectURI: "https://app.example.com/",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 2)],
  softwareVersion: "1.2.0",
}

function serve(fake: FakeSubstrate, version = "v0.121.0") {
  const metadata = {
    issuer: fake.origin,
    authorization_endpoint: `${fake.origin}/oauth/authorize`,
    token_endpoint: `${fake.origin}/oauth/token`,
    scopes_supported: ["read:*", "write:*"],
    authorization_details_types_supported: ["package"],
  }
  vi.stubGlobal(
    "fetch",
    vi.fn((input: string, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/.well-known/oauth-authorization-server"))
        return Promise.resolve(new Response(JSON.stringify(metadata)))
      if (url.endsWith("/.well-known/substrate/server.json"))
        return Promise.resolve(
          new Response(
            JSON.stringify({
              server: { version },
              features: [{ name: "apps" }],
            })
          )
        )
      return fake.fetch(input, init)
    })
  )
}

const now = Date.parse("2026-11-08T12:00:00Z")
const session = (over: Partial<Session> = {}): Session => ({
  server: "https://substrate.example.com",
  repository: "ada.example.org",
  token: "tok",
  scopes: manifest.scopes,
  requestedScopes: manifest.scopes,
  clientID: manifest.clientID,
  details: [],
  signedInAt: now - 86_400_000,
  ...over,
})

describe("checkAppStatus", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("is clear when the token holds what the app asks for", async () => {
    const fake = new FakeSubstrate()
    fake.checkResult = {
      scopes: { missing: [] },
      setup: [
        {
          type: "package",
          action: "none",
          package: {
            id: "substrate.reamde.dev/template",
            required: 2,
            held: 2,
          },
        },
      ],
    }
    serve(fake)
    const status = await checkAppStatus({
      manifest,
      session: session(),
      now: () => now,
    })
    expect(status.issues).toEqual([])
    expect(status.serverVersion).toBe("v0.121.0")
    expect(status.packages).toEqual([
      {
        id: "substrate.reamde.dev/template",
        name: "Template",
        needs: 2,
        held: 2,
      },
    ])
    const check = fake.requests.find(
      (r) => r.path === "/api/v1/authorizations/check"
    )
    expect(check?.body).toEqual({
      scope: "write:substrate.reamde.dev/template/*",
      details: manifest.details,
      softwareVersion: "1.2.0",
    })
  })

  it("asks for an approval when the package fell behind or a scope is missing", async () => {
    const fake = new FakeSubstrate()
    fake.checkResult = {
      scopes: { missing: ["read:substrate.reamde.dev/tasks/*"] },
      setup: [
        {
          type: "package",
          action: "upgrade",
          package: {
            id: "substrate.reamde.dev/template",
            required: 2,
            held: 1,
            shipped: 2,
          },
        },
      ],
    }
    serve(fake)
    const status = await checkAppStatus({
      manifest,
      session: session(),
      now: () => now,
    })
    expect(status.issues).toEqual([
      {
        type: "needsApproval",
        changes: [
          {
            kind: "scope",
            words:
              "See your records of the tasks package, including kinds added later",
            detail: "read:substrate.reamde.dev/tasks/*",
          },
          {
            kind: "upgrade",
            words: "Update Template",
            detail: "Version 1 to 2",
          },
        ],
      },
    ])
  })

  it("says a package only a newer server ships is a server update", async () => {
    const fake = new FakeSubstrate()
    fake.checkResult = {
      scopes: { missing: [] },
      setup: [
        {
          type: "package",
          action: "install",
          blocked: {
            reason: "server-update",
            problem: "This server ships version 1.",
          },
          package: { id: "substrate.reamde.dev/template", required: 2 },
        },
      ],
    }
    serve(fake)
    const status = await checkAppStatus({
      manifest,
      session: session(),
      now: () => now,
    })
    expect(status.issues[0]).toMatchObject({
      type: "serverTooOld",
      blocked: [{ words: "Install Template version 2 or later" }],
    })
  })

  it("says a server too old for the app before it asks it anything", async () => {
    const fake = new FakeSubstrate()
    serve(fake)
    const status = await checkAppStatus({
      manifest: { ...manifest, requirement: { minimumVersion: "0.200.0" } },
      session: session(),
      now: () => now,
    })
    expect(status.issues).toEqual([
      {
        type: "serverTooOld",
        running: "v0.121.0",
        needed: "0.200.0",
        missing: [],
        blocked: [],
      },
    ])
    expect(fake.requests).toHaveLength(0)
  })

  it("leaves a scope the owner left out unasked", async () => {
    const fake = new FakeSubstrate()
    serve(fake)
    const asked = [
      "write:substrate.reamde.dev/template/*",
      "read:substrate.reamde.dev/tasks/*",
    ]
    await checkAppStatus({
      manifest: { ...manifest, scopes: asked },
      session: session({ requestedScopes: asked, scopes: [asked[0]] }),
      now: () => now,
    })
    const check = fake.requests.find(
      (r) => r.path === "/api/v1/authorizations/check"
    )
    expect((check?.body as { scope: string }).scope).toBe(asked[0])
  })

  it("tells an ending soon, and an ending past, apart", async () => {
    const fake = new FakeSubstrate()
    serve(fake)
    const soon = now + 3600_000
    const ending = await checkAppStatus({
      manifest,
      session: session({ expiresAt: soon }),
      now: () => now,
    })
    expect(ending.issues).toEqual([{ type: "accessEndsSoon", at: soon }])
    const ended = await checkAppStatus({
      manifest,
      session: session({ expiresAt: now - 1 }),
      now: () => now,
    })
    expect(ended.issues).toEqual([{ type: "accessEnded" }])
  })

  it("reads a 401 from the check as access ended", async () => {
    const fake = new FakeSubstrate()
    fake.fail({ path: /authorizations\/check/ }, 401, {
      error: { code: "auth", message: "no" },
    })
    serve(fake)
    const status = await checkAppStatus({
      manifest,
      session: session(),
      now: () => now,
    })
    expect(status.issues).toEqual([{ type: "accessEnded" }])
  })

  it("keeps the ending window to a quarter of the approved lifetime", () => {
    const week = 7 * 86_400_000
    expect(
      endingWindow(session({ signedInAt: now, expiresAt: now + week }))
    ).toBe(week / 4)
    expect(
      endingWindow(
        session({ signedInAt: now, expiresAt: now + 90 * 86_400_000 })
      )
    ).toBe(week)
  })
})
