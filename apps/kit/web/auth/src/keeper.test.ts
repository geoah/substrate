import { FakeLocks, FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NotSignedIn, NotSignedOut, SessionKeeper } from "./keeper"
import { sessionLocks, tabLocks } from "./locks"
import type { Session } from "./session"

const sessionKey = "substrate.session.template"
const lockName = sessionKey
const queueKey = "substrate.unrevoked.template"
let fake: FakeSubstrate

const session = (token: string, over: Partial<Session> = {}): Session => ({
  server: fake.origin,
  repository: "ada.example.org",
  token,
  scopes: [],
  clientID: "https://app.example.com/client.json",
  details: [],
  signedInAt: 0,
  ...over,
})

/** A tab of the app: its own keeper, over the storage every tab shares. */
const tab = (retryDelays = [10]) =>
  new SessionKeeper("template", { retryDelays })

const keep = (s: Session) => localStorage.setItem(sessionKey, JSON.stringify(s))
const stored = () =>
  (JSON.parse(localStorage.getItem(sessionKey) ?? "null") as Session | null)
    ?.token
const queued = () =>
  (JSON.parse(localStorage.getItem(queueKey) ?? "[]") as Session[]).map(
    (s) => s.token
  )

/** Each token is the kept session's, queued to revoke, or revoked. */
function accounted(...tokens: string[]) {
  for (const t of tokens)
    expect(
      stored() === t || queued().includes(t) || fake.revoked.has(t),
      `${t} is held`
    ).toBe(true)
}

/** Storage that will not take these keys, as a full one does. */
function refuse(...keys: string[]) {
  const real = Storage.prototype.setItem
  return vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
    this: Storage,
    k: string,
    v: string
  ) {
    if (keys.includes(k)) throw new DOMException("full", "QuotaExceededError")
    real.call(this, k, v)
  })
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
      lockName,
      () => new Promise<void>((release) => granted(release))
    )
  })
}

describe.each([
  { lock: "Web Locks", web: true },
  { lock: "the tab's own lock, without Web Locks", web: false },
])("SessionKeeper, under $lock", ({ web }) => {
  let locks: FakeLocks | undefined
  let uninstall: () => void

  beforeEach(() => {
    localStorage.clear()
    fake = new FakeSubstrate()
    vi.stubGlobal("fetch", fake.fetch)
    locks = web ? new FakeLocks() : undefined
    uninstall = locks?.install() ?? (() => {})
  })
  afterEach(() => {
    uninstall()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("takes the one lock every tab of the app shares", async () => {
    await tab().signIn(session("a"))
    if (locks) expect(locks.granted).toContain(lockName)
    else expect(sessionLocks()).toBe(tabLocks)
  })

  it("queues the session a sign-in replaces before saving it, and never waits on its revocation", async () => {
    keep(session("a"))
    const release = revocationWaits("a")
    await tab().signIn(session("c"))
    expect(stored()).toBe("c")
    expect(queued()).toEqual(["a"])
    expect(fake.revoked.has("a")).toBe(false)
    release()
    await until(() => fake.revoked.has("a") && !queued().length)
  })

  it.each(["the other tab's sign-in first", "this tab's sign-in first"])(
    "never overwrites a session another tab installed during a sign-in's return unqueued: %s",
    async (first) => {
      keep(session("a"))
      // A's revocation stays pending throughout.
      const release = revocationWaits("a")
      const [here, there] = [tab(), tab()]
      const unlock = await holdLock()
      const runs =
        first === "the other tab's sign-in first"
          ? [there.signIn(session("b")), here.signIn(session("c"))]
          : [here.signIn(session("c")), there.signIn(session("b"))]
      unlock()
      await Promise.all(runs)
      const last = first === "the other tab's sign-in first" ? "c" : "b"
      expect(stored()).toBe(last)
      accounted("a", "b", "c")
      await until(() => fake.revoked.has(last === "c" ? "b" : "c"))
      expect(queued()).toEqual(["a"])
      release()
      await until(() => fake.revoked.has("a") && !queued().length)
      expect(fake.revoked.has(last)).toBe(false)
    }
  )

  it("refuses a stale tab's sign-out it can't queue, with the server unreachable, and loses nothing", async () => {
    keep(session("b")) // another tab signed in since
    const full = refuse(queueKey)
    fake.offline = true
    const here = tab()
    await expect(here.signOut(session("a"))).rejects.toBeInstanceOf(
      NotSignedOut
    )
    expect(stored()).toBe("b")
    expect(queued()).toEqual([])

    // A is still the tab's to sign out, once the browser keeps it.
    full.mockRestore()
    expect(await here.signOut(session("a"))).toEqual({
      type: "replaced",
      session: session("b"),
    })
    expect(stored()).toBe("b")
    expect(queued()).toEqual(["a"])
    fake.offline = false
    expect(await here.send()).toEqual([])
    expect([...fake.revoked]).toEqual(["a"])
    expect(stored()).toBe("b")
  })

  it("refuses a sign-out it can't queue and leaves the session in use", async () => {
    keep(session("a"))
    refuse(queueKey)
    await expect(tab().signOut(session("a"))).rejects.toThrow(
      "You're still signed in"
    )
    expect(stored()).toBe("a")
    expect(fake.revoked.size).toBe(0)
  })

  it("keeps the session in use when a new one can't be saved, and revokes the new token from the queue", async () => {
    keep(session("a"))
    refuse(sessionKey)
    const out = tab().signIn(session("c"))
    await expect(out).rejects.toBeInstanceOf(NotSignedIn)
    await expect(out).rejects.toMatchObject({ access: "queued" })
    expect(stored()).toBe("a")
    accounted("a", "c")
    await until(() => fake.revoked.has("c") && !queued().length)
    expect(fake.revoked.has("a")).toBe(false)
  })

  it("revokes a new token at once where neither it nor the session it replaces can be kept, and says when that fails", async () => {
    keep(session("a"))
    refuse(sessionKey, queueKey)
    await expect(tab().signIn(session("c"))).rejects.toMatchObject({
      access: "revoked",
    })
    expect(stored()).toBe("a")
    expect([...fake.revoked]).toEqual(["c"])

    fake.offline = true
    await expect(tab().signIn(session("d"))).rejects.toMatchObject({
      name: "NotSignedIn",
      access: "live",
      message: expect.stringContaining("revoke it yourself") as string,
    })
    expect(stored()).toBe("a")
  })

  it("refuses a sign-in whose replaced token can't be queued, though the new session could be saved", async () => {
    keep(session("a"))
    refuse(queueKey)
    await expect(tab().signIn(session("c"))).rejects.toMatchObject({
      access: "revoked",
    })
    expect(stored()).toBe("a")
    expect(fake.revoked.has("a")).toBe(false)
  })

  it.each([
    ["the sign-out first", ["out", "in"]],
    ["the sign-in first", ["in", "out"]],
  ])(
    "lets two tabs sign out and sign in at once and holds every token: %s",
    async (_order, steps) => {
      keep(session("a"))
      // Nothing is confirmed, so every token must stay held here.
      fake.offline = true
      const [one, two] = [tab(), tab()]
      const unlock = await holdLock()
      const runs = steps.map((step) =>
        step === "out" ? one.signOut(session("a")) : two.signIn(session("c"))
      )
      unlock()
      const answers = await Promise.all(runs)
      const out = answers[steps.indexOf("out")]
      expect(out).toEqual(
        steps[0] === "out"
          ? { type: "signedOut" }
          : { type: "replaced", session: session("c") }
      )
      expect(stored()).toBe("c")
      expect(queued()).toEqual(["a"])
      accounted("a", "c")
      if (locks)
        expect(
          locks.granted.filter((n) => n === lockName).length
        ).toBeGreaterThanOrEqual(2)

      fake.offline = false
      expect(await one.send()).toEqual([])
      expect([...fake.revoked]).toEqual(["a"])
      expect(stored()).toBe("c")
    }
  )

  it("takes up a 401 only while the session is still the kept one", async () => {
    keep(session("b"))
    const here = tab()
    expect((await here.end(session("a")))?.token).toBe("b")
    expect(stored()).toBe("b")
    expect(await here.end(session("b"))).toBeNull()
    expect(stored()).toBeUndefined()
    expect(queued()).toEqual([])
  })

  it("sends a revocation again after a wait while the server does not confirm it", async () => {
    keep(session("a"))
    const here = tab([20])
    const heard: string[][] = []
    const stop = here.onUnrevoked((left) =>
      heard.push(left.map((s) => s.token))
    )
    fake.offline = true
    await here.signOut(session("a"))
    await until(() => heard.length > 0, 2000, "the first pass")
    expect(heard[0]).toEqual(["a"])
    fake.offline = false
    await until(() => heard.at(-1)?.length === 0, 2000, "the retry")
    expect([...fake.revoked]).toEqual(["a"])
    expect(queued()).toEqual([])
    stop()
  })

  it("counts only a 2xx as revoked: a 400 or a 401 leaves the token queued", async () => {
    const here = tab()
    localStorage.setItem(queueKey, JSON.stringify([session("a")]))
    for (const status of [400, 401]) {
      fake.fail({ path: /oauth\/revoke/ }, status, {
        error: "invalid_client",
      })
      expect((await here.send()).map((s) => s.token)).toEqual(["a"])
      expect(queued()).toEqual(["a"])
    }
  })

  it("sends a queued token whose time is up, since only the server's answer confirms it gone", async () => {
    localStorage.setItem(
      queueKey,
      JSON.stringify([session("old", { expiresAt: 1000 })])
    )
    expect(await tab().send()).toEqual([])
    expect(fake.revoked.has("old")).toBe(true)
    expect(queued()).toEqual([])
  })

  it("keeps custody of a token whose time looks up only by this browser's clock", async () => {
    const now = Date.now()
    const live = (token: string) =>
      session(token, { expiresAt: now + 3_600_000 })
    // The clock runs two hours ahead of the server's.
    vi.spyOn(Date, "now").mockReturnValue(now + 7_200_000)
    const here = tab()

    // A sign-out queues it and the server is asked.
    keep(live("a"))
    expect(await here.signOut(live("a"))).toEqual({ type: "signedOut" })
    await until(() => fake.revoked.has("a"), 2000, "a revoked")
    expect(queued()).toEqual([])

    // A time up queues it before the session is cleared.
    keep(live("b"))
    expect(await here.end(live("b"), "timeUp")).toBeNull()
    expect(stored()).toBeUndefined()
    await until(() => fake.revoked.has("b"), 2000, "b revoked")

    // Where the queue will not take it, the session stays kept, and the
    // next sign-in queues it.
    keep(live("c"))
    const full = refuse(queueKey)
    expect(await here.end(live("c"), "timeUp")).toBeNull()
    expect(stored()).toBe("c")
    expect(here.load()).toBeNull()
    full.mockRestore()
    await here.signIn(session("d"))
    expect(stored()).toBe("d")
    accounted("c", "d")
    await until(() => fake.revoked.has("c"), 2000, "c revoked")
  })

  it("leaves nothing queued for a 401: the server's word is the confirmation", async () => {
    keep(session("a"))
    expect(await tab().end(session("a"), "refused")).toBeNull()
    expect(stored()).toBeUndefined()
    expect(queued()).toEqual([])
  })

  it("retries each token on its own while another's revocation hangs, and gives the hung one up in time", async () => {
    localStorage.setItem(queueKey, JSON.stringify([session("a"), session("b")]))
    const release = revocationWaits("a")
    fake.fail({ path: /oauth\/revoke/ }, 503)
    const here = new SessionKeeper("template", {
      retryDelays: [10],
      revokeTimeout: 200,
    })
    const heard: string[][] = []
    const stop = here.onUnrevoked((left) =>
      heard.push(left.map((s) => s.token))
    )
    void here.send()
    // b's 503 is told and sent again while a is still on the wire.
    await until(() => fake.revoked.has("b"), 2000, "b sent again")
    expect(heard[0]).toEqual(["b"])
    expect(queued()).toEqual(["a"])
    // a's request runs out of time, counts as not confirmed, and is told.
    await until(() => heard.some((l) => l.includes("a")), 2000, "a timed out")
    release()
    await until(() => fake.revoked.has("a"), 2000, "a revoked")
    await until(() => queued().length === 0, 2000, "the queue drained")
    stop()
  })

  it("never revokes the token the kept session holds", async () => {
    keep(session("a"))
    localStorage.setItem(queueKey, JSON.stringify([session("a")]))
    expect(await tab().send()).toEqual([])
    expect(fake.revoked.size).toBe(0)
    expect(queued()).toEqual([])
    expect(stored()).toBe("a")
  })

  it("queues a token a sign-in won't keep, or revokes it at once", async () => {
    const here = tab()
    expect(await here.discard(session("x"))).toBe("queued")
    await until(() => fake.revoked.has("x"))
    const full = refuse(queueKey)
    expect(await here.discard(session("y"))).toBe("revoked")
    fake.offline = true
    expect(await here.discard(session("z"))).toBe("live")
    full.mockRestore()
  })
})
