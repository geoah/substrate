/** The block the build writes and the shell reads: a whole one parses, one
 * short an import map entry or malformed does not, and the pin names every
 * path behind the shell's origin with the two tokens the static policy also
 * carries. */

import { describe, expect, it } from "vitest"

import { parseGuestBuild, pinPolicy, type GuestBuild } from "./guest-build"

const BUILD: GuestBuild = {
  imports: {
    react: "/assets/app-react-Ab12Cd34.js",
    "react/jsx-runtime": "/assets/app-jsx-runtime-Ef56Gh78.js",
    "react-dom/client": "/assets/app-react-dom-client-Ij90Kl12.js",
    "substrate/app": "/assets/app-sdk-Mn34Op56.js",
    "substrate/ui": "/assets/app-ui-Qr78St90.js",
  },
  scripts: ["/assets/app-frame-Uv12Wx34.js", "/assets/react-Yz56Ab78.js"],
  fonts: ["/assets/geist-latin-wght-normal-Cd90Ef12.woff2"],
}

describe("parseGuestBuild", () => {
  it("reads a whole block back", () => {
    expect(parseGuestBuild(JSON.stringify(BUILD))).toEqual(BUILD)
  })

  it("refuses an absent, malformed or incomplete block", () => {
    expect(parseGuestBuild(undefined)).toBeUndefined()
    expect(parseGuestBuild("")).toBeUndefined()
    expect(parseGuestBuild("{")).toBeUndefined()
    expect(parseGuestBuild("[]")).toBeUndefined()
    const short = Object.fromEntries(
      Object.entries(BUILD.imports).filter(([k]) => k !== "react")
    )
    expect(
      parseGuestBuild(JSON.stringify({ ...BUILD, imports: short }))
    ).toBeUndefined()
    expect(
      parseGuestBuild(JSON.stringify({ ...BUILD, scripts: [1] }))
    ).toBeUndefined()
    expect(
      parseGuestBuild(JSON.stringify({ ...BUILD, fonts: undefined }))
    ).toBeUndefined()
  })
})

describe("pinPolicy", () => {
  it("puts the origin before every path and keeps inline and blob", () => {
    expect(pinPolicy(BUILD, "https://console.example.com")).toBe(
      "script-src https://console.example.com/assets/app-frame-Uv12Wx34.js https://console.example.com/assets/react-Yz56Ab78.js 'unsafe-inline' blob:; " +
        "font-src https://console.example.com/assets/geist-latin-wght-normal-Cd90Ef12.woff2"
    )
  })

  it("never names 'self'", () => {
    expect(pinPolicy(BUILD, "http://localhost:5173")).not.toContain("'self'")
  })
})
