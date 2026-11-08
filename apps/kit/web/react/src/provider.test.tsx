import type { AppManifest, Session } from "@substrate/auth"
import { defineKind, stringProp } from "@substrate/client"
import { FakeSubstrate, until } from "@substrate/testing"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { StrictMode, useContext, useEffect } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SubstrateContext, useConnection } from "./context"
import { useRecords } from "./hooks"
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
const Item = defineKind(kind, { read: (r) => stringProp(r, "name") ?? "" })
const sessionKey = "substrate.session.template"
const queueKey = "substrate.unrevoked.template"
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

let seen: {
  phase?: string
  open: boolean
  token?: string
  signOut?: () => Promise<void>
}
let names: readonly string[] = []
function Probe() {
  const { phase } = useConnection()
  const open = useContext(SubstrateContext)
  useEffect(() => {
    seen = {
      phase,
      open: !!open,
      token: open?.session.token,
      signOut: open?.signOut,
    }
  })
  return open ? <Names /> : null
}
function Names() {
  const records = useRecords(Item)
  useEffect(() => {
    names = records
  })
  return null
}

/** The IndexedDB connections opened from here on and not closed yet. */
function connections(): Set<IDBDatabase> {
  const open = new Set<IDBDatabase>()
  const real = indexedDB.open.bind(indexedDB)
  vi.spyOn(indexedDB, "open").mockImplementation((name, version) => {
    const req = real(name, version)
    req.addEventListener("success", () => {
      const db = req.result
      open.add(db)
      const close = db.close.bind(db)
      db.close = () => {
        open.delete(db)
        close()
      }
    })
    return req
  })
  return open
}

const databases = async () => (await indexedDB.databases()).map((d) => d.name)
const database = () => `substrate.template.ada${n}.example.org`

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
  names = []
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

  it("revokes a stale tab's own token on sign-out and leaves the session another tab signed in since", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    // Another tab signs in again; this one has not heard it yet.
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-b")))
    await act(async () => {
      await seen.signOut?.()
    })
    expect(JSON.parse(localStorage.getItem(sessionKey)!).token).toBe("tok-b")
    expect([...fake.revoked]).toEqual(["tok-a"])
    expect(localStorage.getItem(queueKey)).toBeNull()
    await waitFor(() => expect(seen.token).toBe("tok-b"), { timeout: 3000 })
    // The repository's data is the other session's too, so it stays.
    expect(await databases()).toContain(database())
  })

  it("stays signed in when the token can be neither kept nor revoked, and signs out on the next try", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    const realSet = Storage.prototype.setItem
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
      this: Storage,
      k: string,
      v: string
    ) {
      if (k === queueKey) throw new DOMException("full", "QuotaExceededError")
      realSet.call(this, k, v)
    })
    fake.offline = true
    let refused: unknown
    await act(async () => {
      await seen.signOut?.().catch((e: unknown) => (refused = e))
    })
    expect(refused).toMatchObject({ name: "NotSignedOut" })
    expect(seen.phase).toBe("open")
    expect(JSON.parse(localStorage.getItem(sessionKey)!).token).toBe("tok-a")

    fake.offline = false
    await act(async () => {
      await seen.signOut?.()
    })
    expect(seen.phase).toBe("signedOut")
    expect(localStorage.getItem(sessionKey)).toBeNull()
    expect([...fake.revoked]).toEqual(["tok-a"])
  })

  it("opens one link under StrictMode, leaks no database connection, and still signs out", async () => {
    const open = connections()
    fake.seed(kind, "x", { name: "Milk" })
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <StrictMode>
        <SubstrateProvider manifest={manifest} kinds={[kind]}>
          <Probe />
        </SubstrateProvider>
      </StrictMode>
    )
    await waitFor(() => expect(names).toEqual(["Milk"]), { timeout: 3000 })
    await until(() => open.size === 1, 2000, "the first mount's link to close")
    expect(fake.openWatches).toBe(1)

    fake.change(kind, "x", { name: "Oat milk" })
    await waitFor(() => expect(names).toEqual(["Oat milk"]))

    await act(async () => {
      await seen.signOut?.()
    })
    expect(seen.phase).toBe("signedOut")
    expect(open.size).toBe(0)
    expect(fake.openWatches).toBe(0)
    expect(await databases()).not.toContain(database())
    expect([...fake.revoked]).toEqual(["tok-a"])
  })
})
