import {
  SessionKeeper,
  sessionLocks,
  type AppManifest,
  type Session,
} from "@substrate/auth"
import { defineKind, stringProp } from "@substrate/client"
import { FakeLocks, FakeSubstrate, until } from "@substrate/testing"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { StrictMode, useContext, useEffect } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SubstrateContext, useConnection } from "./context"
import { useRecords } from "./hooks"
import { SubstrateProvider } from "./provider"
import { ToastHost } from "./toast"

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

const stored = () =>
  (JSON.parse(localStorage.getItem(sessionKey) ?? "null") as Session | null)
    ?.token
const queued = () =>
  (JSON.parse(localStorage.getItem(queueKey) ?? "[]") as Session[]).map(
    (s) => s.token
  )

/** This page loads as the server's redirect back, which mints `token`. */
function returning(token: string) {
  sessionStorage.setItem(
    "substrate.signin.template",
    JSON.stringify({
      state: "s",
      verifier: "v",
      server: fake.origin,
      metadata: {
        issuer: fake.origin,
        authorization_endpoint: `${fake.origin}/oauth/authorize`,
        token_endpoint: `${fake.origin}/oauth/token`,
        revocation_endpoint: `${fake.origin}/oauth/revoke`,
      },
      scopes: manifest.scopes,
      startedAt: 0,
    })
  )
  window.history.replaceState(null, "", "/?code=c&state=s")
  fake.repository = `ada${n}.example.org`
  fake.mint.push(token)
}

/** `token`'s revocations stay on the wire until let go. */
function revocationWaits(token: string): () => void {
  let open!: () => void
  const gate = new Promise<void>((r) => (open = r))
  vi.stubGlobal("fetch", async (input: string, init: RequestInit = {}) => {
    const body = new URLSearchParams(init.body as URLSearchParams)
    if (
      new URL(input).pathname === "/oauth/revoke" &&
      body.get("token") === token
    )
      await gate
    return fake.fetch(input, init)
  })
  return open
}

/** Takes the session lock as another tab's change would, until let go. */
function holdLock(): Promise<() => void> {
  return new Promise((granted) => {
    void sessionLocks().request(
      sessionKey,
      () => new Promise<void>((release) => granted(release))
    )
  })
}

beforeEach(() => {
  n++
  localStorage.clear()
  sessionStorage.clear()
  window.history.replaceState(null, "", "/")
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

  it("sends no token once another tab's session replaced this one's, and takes that one up at the 401", async () => {
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
    // The read's 401 ends tok-a here and leaves the other tab's session.
    await waitFor(() => expect(seen.token).toBe("tok-b"), { timeout: 3000 })
    expect(stored()).toBe("tok-b")
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
    expect(stored()).toBe("tok-b")
    // Revoked in the background, after the sign-out.
    await waitFor(() => expect([...fake.revoked]).toEqual(["tok-a"]))
    await waitFor(() => expect(localStorage.getItem(queueKey)).toBeNull())
    await waitFor(() => expect(seen.token).toBe("tok-b"), { timeout: 3000 })
    // The repository's data is the other session's too, so it stays.
    expect(await databases()).toContain(database())
  })

  it("stays signed in when the browser can't keep the token to revoke, and signs out once it can", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    const realSet = Storage.prototype.setItem
    const full = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(function (this: Storage, k: string, v: string) {
        if (k === queueKey) throw new DOMException("full", "QuotaExceededError")
        realSet.call(this, k, v)
      })
    // Refused though the server could revoke it now: a token leaves the
    // session only into the queue.
    let refused: unknown
    await act(async () => {
      await seen.signOut?.().catch((e: unknown) => (refused = e))
    })
    expect(refused).toMatchObject({ name: "NotSignedOut" })
    expect(seen.phase).toBe("open")
    expect(stored()).toBe("tok-a")
    expect(fake.revoked.size).toBe(0)

    full.mockRestore()
    await act(async () => {
      await seen.signOut?.()
    })
    expect(seen.phase).toBe("signedOut")
    expect(localStorage.getItem(sessionKey)).toBeNull()
    await waitFor(() => expect([...fake.revoked]).toEqual(["tok-a"]))
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
    await waitFor(() => expect([...fake.revoked]).toEqual(["tok-a"]))
  })

  it("opens a renewed sign-in without waiting on the old token's revocation", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    const release = revocationWaits("tok-a")
    returning("tok-c")
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    expect(seen.token).toBe("tok-c")
    expect(stored()).toBe("tok-c")
    expect(queued()).toEqual(["tok-a"])
    expect(fake.revoked.has("tok-a")).toBe(false)
    release()
    await waitFor(() => expect(queued()).toEqual([]))
    expect([...fake.revoked]).toEqual(["tok-a"])
  })

  it("shows a sign-in this browser couldn't keep, and keeps the session before", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    returning("tok-c")
    const realSet = Storage.prototype.setItem
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
      this: Storage,
      k: string,
      v: string
    ) {
      if (k === sessionKey) throw new DOMException("full", "QuotaExceededError")
      realSet.call(this, k, v)
    })
    render(
      <ToastHost>
        <SubstrateProvider manifest={manifest} kinds={[kind]}>
          <Probe />
        </SubstrateProvider>
      </ToastHost>
    )
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    expect(seen.token).toBe("tok-a")
    expect(stored()).toBe("tok-a")
    await waitFor(() =>
      expect(document.body.textContent).toContain(
        "This browser couldn't keep the sign-in"
      )
    )
    await waitFor(() => expect([...fake.revoked]).toEqual(["tok-c"]))
  })
})

describe.each([
  { lock: "Web Locks", web: true },
  { lock: "the tab's own lock, without Web Locks", web: false },
])("SubstrateProvider's sign-in return, under $lock", ({ web }) => {
  let uninstall: () => void
  beforeEach(() => {
    uninstall = web ? new FakeLocks().install() : () => {}
  })
  afterEach(() => uninstall())

  it("never overwrites a session another tab installed while it returned and the old token's revocation was pending", async () => {
    localStorage.setItem(sessionKey, JSON.stringify(session("tok-a")))
    // tok-a's revocation stays pending throughout.
    const release = revocationWaits("tok-a")
    const signIns = vi.spyOn(SessionKeeper.prototype, "signIn")
    // Another tab's change holds the lock while this page returns, and
    // installs tok-b before this page's sign-in reads the kept session.
    const unlock = await holdLock()
    const other = new SessionKeeper(manifest.id).signIn(session("tok-b"))
    returning("tok-c")
    render(
      <SubstrateProvider manifest={manifest} kinds={[kind]}>
        <Probe />
      </SubstrateProvider>
    )
    await waitFor(() => expect(signIns).toHaveBeenCalledTimes(2))
    unlock()
    await other
    await waitFor(() => expect(seen.phase).toBe("open"), { timeout: 3000 })
    expect(seen.token).toBe("tok-c")
    expect(stored()).toBe("tok-c")
    // tok-b, installed between, was queued before tok-c took its place.
    await waitFor(() => expect(fake.revoked.has("tok-b")).toBe(true))
    expect(queued()).toEqual(["tok-a"])
    release()
    await waitFor(() => expect(queued()).toEqual([]))
    expect([...fake.revoked].sort()).toEqual(["tok-a", "tok-b"])
  })
})
