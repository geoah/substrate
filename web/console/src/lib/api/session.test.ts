// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"

import {
  clearSession,
  getIdentityName,
  getRepository,
  getToken,
  getTokenId,
  hasSession,
  retireStoredSession,
  saveSession,
  sessionExpired,
  setRepository,
  setSessionChangedHandler,
  setToken,
  setUnauthorizedHandler,
} from "./session"

describe("session", () => {
  beforeEach(() => {
    setSessionChangedHandler(null)
    clearSession()
    setUnauthorizedHandler(null)
  })

  it("stores and clears the token", () => {
    expect(hasSession()).toBe(false)
    setToken("substrate_tok_x")
    expect(getToken()).toBe("substrate_tok_x")
    expect(hasSession()).toBe(true)
    clearSession()
    expect(getToken()).toBeNull()
  })

  it("keeps the person's name beside the repository, and reads the repository's for a session that kept none", () => {
    saveSession(
      "substrate_tok_x",
      "notes.example.com",
      "tok-1",
      "ada.example.com"
    )
    expect(getRepository()).toBe("notes.example.com")
    expect(getIdentityName()).toBe("ada.example.com")
    saveSession("substrate_tok_y", "notes.example.com", "tok-2")
    expect(getIdentityName()).toBe("notes.example.com")
    clearSession()
    expect(getIdentityName()).toBeNull()
    expect(localStorage.getItem("substrate.identity")).toBeNull()
  })

  it("sessionExpired drops the token and fires the handler", () => {
    setToken("substrate_tok_x")
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    sessionExpired()
    expect(getToken()).toBeNull()
    expect(handler).toHaveBeenCalledOnce()
  })

  it("sessionExpired without a handler still drops the token", () => {
    setToken("substrate_tok_x")
    expect(() => sessionExpired()).not.toThrow()
    expect(getToken()).toBeNull()
  })
  // Every cached answer belongs to one repository and no query key names
  // which, so signing in as somebody else must drop the cache. These pin the
  // signal that does it: a sidebar built from the previous repository's kinds
  // links collections this one never imported.
  it("fires the session-changed handler on every identity write", () => {
    const changed = vi.fn()
    setSessionChangedHandler(changed)

    setToken("substrate_tok_probe")
    expect(changed).toHaveBeenCalledTimes(1)

    saveSession("substrate_tok_second", "ada", "tok-1")
    expect(changed).toHaveBeenCalledTimes(2)

    clearSession()
    expect(changed).toHaveBeenCalledTimes(3)
  })

  it("fires the session-changed handler when a 401 drops the session", () => {
    saveSession("substrate_tok_first", "ada", "tok-1")
    const changed = vi.fn()
    setSessionChangedHandler(changed)

    sessionExpired()
    expect(changed).toHaveBeenCalledTimes(1)
    expect(hasSession()).toBe(false)
  })
})

describe("session in the Apple app", () => {
  const post = vi.fn()
  const SESSION = {
    secret: "substrate_tok_app",
    repository: "ada.example.com",
    tokenId: "tok-app",
  }

  beforeEach(() => {
    setSessionChangedHandler(null)
    setUnauthorizedHandler(null)
    vi.unstubAllGlobals()
    localStorage.clear()
    post.mockReset()
    vi.stubGlobal("substrateShell", {
      platform: "ios",
      server: "https://substrate.example.com",
      session: SESSION,
    })
    vi.stubGlobal("webkit", {
      messageHandlers: { substrateShell: { postMessage: post } },
    })
  })

  it("reads the app's sign-in and keeps none of its own", () => {
    expect(getToken()).toBe("substrate_tok_app")
    expect(getRepository()).toBe("ada.example.com")
    // An app that gives no person's name: theirs is the repository's.
    expect(getIdentityName()).toBe("ada.example.com")
    expect(getTokenId()).toBe("tok-app")
    expect(hasSession()).toBe(true)
    expect(localStorage.getItem("substrate.token")).toBeNull()
  })

  it("takes a rename at once, tells the app, keeps it across a navigation, and reads the app's name after a reload", async () => {
    const shell = (session: typeof SESSION) => ({
      platform: "ios",
      server: "https://substrate.example.com",
      session,
    })
    vi.stubGlobal(
      "substrateShell",
      shell({ ...SESSION, secret: "substrate_tok_renamed" })
    )
    setRepository("lovelace.example.com")
    // The page reads the new name at once, for every request after, while
    // the app's frozen session still says the old one.
    expect(getRepository()).toBe("lovelace.example.com")
    expect(getToken()).toBe("substrate_tok_renamed")
    expect(post).toHaveBeenCalledWith({
      type: "renamed",
      tokenId: "tok-app",
      repository: "lovelace.example.com",
    })
    expect(localStorage.getItem("substrate.repository")).toBeNull()
    // A navigation inside the page keeps the page's state, and the name.
    window.history.pushState({}, "", "/settings")
    expect(getRepository()).toBe("lovelace.example.com")
    // The rename is that sign-in's: another is read as the app gives it.
    vi.stubGlobal(
      "substrateShell",
      shell({ ...SESSION, secret: "substrate_tok_next" })
    )
    expect(getRepository()).toBe("ada.example.com")
    // A reload starts the page's modules again: nothing of the page's own
    // survives it, and the name is the one the app's session gives.
    vi.resetModules()
    vi.stubGlobal(
      "substrateShell",
      shell({
        ...SESSION,
        secret: "substrate_tok_renamed",
        repository: "lovelace.example.com",
      })
    )
    const reloaded = await import("./session")
    expect(reloaded.getRepository()).toBe("lovelace.example.com")
    expect(reloaded.getToken()).toBe("substrate_tok_renamed")
    // So the app has to keep it: a reload under the session it had before
    // the rename reads the old name, which the server still answers to.
    vi.resetModules()
    vi.stubGlobal(
      "substrateShell",
      shell({ ...SESSION, secret: "substrate_tok_renamed" })
    )
    const stale = await import("./session")
    expect(stale.getRepository()).toBe("ada.example.com")
  })

  it("tells the app which sign-in a 401 ended, once, and reads it no more", () => {
    vi.stubGlobal("substrateShell", {
      platform: "ios",
      server: "https://substrate.example.com",
      session: { ...SESSION, secret: "substrate_tok_ended" },
    })
    const expired = vi.fn()
    setUnauthorizedHandler(expired)
    sessionExpired()
    expect(post).toHaveBeenCalledWith({ type: "signedOut", tokenId: "tok-app" })
    expect(expired).toHaveBeenCalledOnce()
    // The login route reads no session, so it shows the door rather than
    // sending the page back to a console on a dead token.
    expect(hasSession()).toBe(false)
    expect(getToken()).toBeNull()
    sessionExpired()
    expect(post).toHaveBeenCalledOnce()
  })

  it("keeps a kept token it cannot revoke without its id", async () => {
    localStorage.setItem("substrate.token", "substrate_tok_old")
    const fetcher = vi.fn<typeof fetch>()
    await retireStoredSession(fetcher)
    expect(fetcher).not.toHaveBeenCalled()
    expect(localStorage.getItem("substrate.token")).toBe("substrate_tok_old")
  })

  it("revokes a token the page kept from before, with itself, and drops it", async () => {
    localStorage.setItem("substrate.token", "substrate_tok_old")
    localStorage.setItem("substrate.repository", "ada.example.com")
    localStorage.setItem("substrate.tokenid", "tok-old")
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }))
    await retireStoredSession(fetcher)
    expect(fetcher).toHaveBeenCalledWith("/tokens/tok-old", {
      method: "DELETE",
      headers: { Authorization: "Bearer substrate_tok_old" },
    })
    expect(localStorage.getItem("substrate.token")).toBeNull()
    expect(localStorage.getItem("substrate.tokenid")).toBeNull()
  })

  it("keeps a kept token the server would not revoke now, for the next start", async () => {
    localStorage.setItem("substrate.token", "substrate_tok_old")
    localStorage.setItem("substrate.tokenid", "tok-old")
    await retireStoredSession(
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 503 }))
    )
    expect(localStorage.getItem("substrate.token")).toBe("substrate_tok_old")
  })

  it("leaves a browser's own session alone", async () => {
    vi.unstubAllGlobals()
    localStorage.setItem("substrate.token", "substrate_tok_browser")
    const fetcher = vi.fn<typeof fetch>()
    await retireStoredSession(fetcher)
    expect(fetcher).not.toHaveBeenCalled()
    expect(getToken()).toBe("substrate_tok_browser")
  })
})
