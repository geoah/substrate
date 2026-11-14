// @vitest-environment jsdom
/** The publishing client: the plan and the publish are one route told apart
 * by `confirm`, a release names the package, and the list reads the
 * releases' items. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { planPublish, publish, release, releases, releasesOf } from "./publish"
import { clearSession, saveSession } from "./session"
import { ApiError, type Release } from "./types"

const RELEASE: Release = {
  package: "ada.example.com/recipes",
  version: 1,
  sha256: "a".repeat(64),
  releasedAt: "2026-11-14T12:00:00Z",
}

describe("the publishing client", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function sent(): { method: string; path: string; body: unknown } {
    const [url, init] = fetchMock.mock.calls.at(-1)!
    return {
      method: init?.method ?? "GET",
      path: String(url),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    }
  }

  it("plans without confirm and publishes with it", async () => {
    fetchMock.mockImplementation(
      async () => new Response(JSON.stringify({}), { status: 200 })
    )
    await planPublish("~/recipes", "ada.example.com")
    expect(sent()).toEqual({
      method: "POST",
      path: "/api/v1/packages/publish",
      body: { package: "~/recipes", authority: "ada.example.com" },
    })
    await publish("~/recipes", "ada.example.com")
    expect(sent().body).toEqual({
      package: "~/recipes",
      authority: "ada.example.com",
      confirm: true,
    })
  })

  it("releases a package and lists the releases", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(RELEASE), { status: 200 })
    )
    expect(await release("ada.example.com/recipes")).toEqual(RELEASE)
    expect(sent()).toEqual({
      method: "POST",
      path: "/api/v1/packages/release",
      body: { package: "ada.example.com/recipes" },
    })
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [RELEASE] }), { status: 200 })
    )
    const all = await releases()
    expect(sent().path).toBe("/api/v1/packages/releases")
    expect(releasesOf(all, "ada.example.com/recipes")).toEqual([RELEASE])
    expect(releasesOf(all, "ada.example.com/notes")).toEqual([])
  })

  it("rejects with the server's refusal", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "conflict",
            message:
              "ada.example.com/recipes has not changed since version 1, its last release",
          },
        }),
        { status: 409 }
      )
    )
    const err = await release("ada.example.com/recipes").catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(409)
    expect((err as ApiError).message).toContain("has not changed")
  })
})
