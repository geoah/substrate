// @vitest-environment jsdom
/** A signed-out reader at `/` sees the deployment's landing page where it
 * sets one, and goes to the sign-in everywhere else, exactly as a substrate
 * with no site does. A signed-in reader never waits on the site. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, saveSession } from "@/lib/api/session"
import { resetSite } from "@/lib/site"
import { shellGate } from "@/router"

async function gate(pathname: string): Promise<unknown> {
  try {
    await shellGate({ pathname, href: pathname })
  } catch (thrown) {
    const r = thrown as { options?: Record<string, unknown> }
    const { to, search } = r.options ?? {}
    return { to, search }
  }
  return undefined
}

function serve(site: unknown) {
  return vi.fn<typeof fetch>(
    async () => new Response(JSON.stringify({ site }), { status: 200 })
  )
}

describe("the shell's gate and the landing page", () => {
  beforeEach(() => {
    resetSite()
    clearSession()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    resetSite()
    clearSession()
  })

  it("lets a signed-out reader stay on / where a landing page is set", async () => {
    vi.stubGlobal(
      "fetch",
      serve({ name: "Crane", landing: "/site/landing.html" })
    )
    expect(await gate("/")).toBeUndefined()
  })

  it("sends a signed-out reader at / to the sign-in without a landing page", async () => {
    vi.stubGlobal("fetch", serve({ name: "Crane" }))
    expect(await gate("/")).toEqual({
      to: "/login",
      search: { redirect: undefined },
    })
  })

  it("sends a signed-out reader at / to the sign-in where no site is set", async () => {
    vi.stubGlobal("fetch", serve(undefined))
    expect(await gate("/")).toEqual({
      to: "/login",
      search: { redirect: undefined },
    })
  })

  it("keeps every other page behind the sign-in, landing or not", async () => {
    const fetchMock = serve({ name: "Crane", landing: "/site/landing.html" })
    vi.stubGlobal("fetch", fetchMock)
    expect(await gate("/search")).toEqual({
      to: "/login",
      search: { redirect: "/search" },
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("lets a signed-in reader through without asking for the site", async () => {
    const fetchMock = serve({ name: "Crane", landing: "/site/landing.html" })
    vi.stubGlobal("fetch", fetchMock)
    saveSession("substrate_tok_owner", "ada.example.com", "tok-owner")
    expect(await gate("/")).toBeUndefined()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
