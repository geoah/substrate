import type { AppManifest, Session } from "@substrate/auth"
import { FakeSubstrate } from "@substrate/testing"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { useContext, useEffect } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SubstrateContext, useConnection } from "./context"
import { SubstrateProvider } from "./provider"

const kind = "substrate.reamde.dev/template/item"
const manifest: AppManifest = {
  id: "template",
  name: "Substrate Template",
  clientID: "https://app.example.com/client.json",
  redirectURI: "https://app.example.com/",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [],
  softwareVersion: "1.0.0",
}
const sessionKey = "substrate.session.template"
let fake: FakeSubstrate
let n = 0

const session = (token: string): Session => ({
  server: fake.origin,
  repository: `ada${n}.example.org`,
  token,
  scopes: manifest.scopes,
  clientID: manifest.clientID,
  details: [],
  signedInAt: 0,
})

let seen: { phase?: string; open: boolean; signOut?: () => Promise<void> }
function Probe() {
  const { phase } = useConnection()
  const open = useContext(SubstrateContext)
  useEffect(() => {
    seen = { phase, open: !!open, signOut: open?.signOut }
  })
  return null
}

function anotherTab(change: () => void) {
  change()
  window.dispatchEvent(new StorageEvent("storage", { key: sessionKey }))
}

beforeEach(() => {
  n++
  localStorage.clear()
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  seen = { open: false }
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("SubstrateProvider", () => {
  it("closes the repository when another tab signs out, and opens the one another tab signs in", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })

    act(() => anotherTab(() => localStorage.removeItem(sessionKey)))
    expect(seen.phase).toBe("signedOut")
    expect(seen.open).toBe(false)

    act(() =>
      anotherTab(() =>
        localStorage.setItem(sessionKey, JSON.stringify(session("tok-b")))
      )
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
  })

  it("sends no token once another tab's session replaced this one's", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    const before = fake.requests.length
    // Written without the event: a tab that slept through the change.
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-b")))
    fake.seed(kind, "x", { name: "Milk" })
    await waitFor(() => expect(fake.requests.length).toBeGreaterThan(before))
    for (const r of fake.requests.slice(before))
      expect(r.headers.get("Authorization")).not.toBe("Bearer tok-a")
  })

  it("keeps the token to revoke before it clears the session", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    const order: string[] = []
    const realSet = Storage.prototype.setItem
    const realRemove = Storage.prototype.removeItem
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
      this: Storage,
      k: string,
      v: string
    ) {
      order.push(`set ${k}`)
      realSet.call(this, k, v)
    })
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(function (
      this: Storage,
      k: string
    ) {
      order.push(`remove ${k}`)
      realRemove.call(this, k)
    })
    fake.offline = true
    await act(async () => {
      await seen.signOut?.()
    })
    expect(order.indexOf("set substrate.unrevoked.template")).toBeLessThan(
      order.indexOf(`remove ${sessionKey}`)
    )
    expect(order).toContain(`remove ${sessionKey}`)
    expect(seen.phase).toBe("signedOut")
    // The server could not be reached, so the token stays kept.
    expect(localStorage.getItem("substrate.unrevoked.template")).toContain(
      "tok-a"
    )
  })
})
