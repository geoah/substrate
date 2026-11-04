// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  applySiteBranding,
  brandedTitle,
  loadSite,
  peekSite,
  readSite,
  resetSite,
} from "./site"

function discovery(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 })
}

describe("readSite", () => {
  it("is null without a block or without a name", () => {
    expect(readSite(undefined)).toBeNull()
    expect(readSite({})).toBeNull()
    expect(readSite({ name: "  ", logo: "/site/logo.svg" })).toBeNull()
  })

  it("keeps the name, the tagline and the two server paths", () => {
    expect(
      readSite({
        name: "Crane",
        tagline: "A home for your people",
        logo: "/site/logo.svg",
        landing: "/site/landing.html",
      })
    ).toEqual({
      name: "Crane",
      tagline: "A home for your people",
      logo: "/site/logo.svg",
      landing: "/site/landing.html",
    })
  })

  it.each([
    "https://evil.example/x.svg",
    "//evil.example/x.svg",
    "site/x.svg",
    "/\\evil",
  ])("drops a path that leaves this server: %s", (path) => {
    expect(readSite({ name: "Crane", logo: path, landing: path })).toEqual({
      name: "Crane",
    })
  })
})

describe("loadSite", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    resetSite()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    resetSite()
  })

  it("asks discovery once per tab", async () => {
    fetchMock.mockResolvedValue(discovery({ site: { name: "Crane" } }))
    await Promise.all([loadSite(), loadSite()])
    expect(await loadSite()).toEqual({ name: "Crane" })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      "/.well-known/substrate/server.json"
    )
    expect(peekSite()).toEqual({ name: "Crane" })
  })

  it("reads no site from a server that names none, or does not answer", async () => {
    fetchMock.mockResolvedValue(discovery({ registration: {} }))
    expect(await loadSite()).toBeNull()
    resetSite()
    fetchMock.mockRejectedValue(new TypeError("offline"))
    expect(await loadSite()).toBeNull()
  })
})

describe("the tab", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    resetSite()
    vi.stubGlobal("fetch", fetchMock)
    document.head.innerHTML =
      '<link rel="icon" type="image/svg+xml" href="/substrate.svg" />'
    document.title = "Substrate Console"
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    resetSite()
  })

  it("keeps the substrate's title and icon without a site", async () => {
    fetchMock.mockResolvedValue(discovery({}))
    applySiteBranding(await loadSite())
    expect(brandedTitle("Sign in")).toBe("Sign in")
    expect(document.title).toBe("Substrate Console")
    expect(
      document.querySelector('link[rel="icon"]')!.getAttribute("href")
    ).toBe("/substrate.svg")
  })

  it("follows the site's name and logo", async () => {
    fetchMock.mockResolvedValue(
      discovery({ site: { name: "Crane", logo: "/site/logo.png" } })
    )
    applySiteBranding(await loadSite())
    expect(brandedTitle("Sign in")).toBe("Sign in · Crane")
    expect(brandedTitle("Crane")).toBe("Crane")
    expect(document.title).toBe("Crane")
    const icon = document.querySelector('link[rel="icon"]')!
    expect(icon.getAttribute("href")).toBe("/site/logo.png")
    expect(icon.getAttribute("type")).toBe("image/png")
  })
})
