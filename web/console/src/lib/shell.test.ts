import { describe, expect, it, vi } from "vitest"

import {
  changeServer,
  nativeShell,
  substrateHostname,
  substrateOrigin,
} from "./shell"

const browser = {
  location: {
    origin: "https://substrate.example.com",
    hostname: "substrate.example.com",
  },
} as unknown as Window

const inShell = {
  ...browser,
  location: { origin: "substrate://console", hostname: "console" },
  substrateShell: { platform: "macos", server: "https://ada.example.net:8443" },
} as unknown as Window

describe("shell", () => {
  it("reads the substrate off the page's own origin in a browser", () => {
    expect(nativeShell(browser)).toBeUndefined()
    expect(substrateOrigin(browser)).toBe("https://substrate.example.com")
    expect(substrateHostname(browser)).toBe("substrate.example.com")
  })

  it("reads the substrate the shell names, never the bundle's origin", () => {
    expect(nativeShell(inShell)?.platform).toBe("macos")
    expect(substrateOrigin(inShell)).toBe("https://ada.example.net:8443")
    expect(substrateHostname(inShell)).toBe("ada.example.net")
  })

  it("asks the shell for its server picker, and is quiet without one", () => {
    const postMessage = vi.fn()
    changeServer({
      webkit: { messageHandlers: { substrateShell: { postMessage } } },
    })
    expect(postMessage).toHaveBeenCalledWith({ type: "changeServer" })
    expect(() => changeServer({})).not.toThrow()
  })
})
