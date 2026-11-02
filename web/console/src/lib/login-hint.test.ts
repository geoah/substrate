import { describe, expect, it } from "vitest"

import { loginHint } from "./login-hint"

describe("loginHint", () => {
  it("keeps a valid repository name, lower-cased", () => {
    expect(loginHint("geoah.example.org")).toBe("geoah.example.org")
    expect(loginHint(" Ada.Example.COM ")).toBe("ada.example.com")
  })

  it.each([
    undefined,
    42,
    "",
    "ada",
    "ada@example.org",
    "-ada.example.org",
    "ada..example.org",
    "<script>.example.org",
    `${"a".repeat(64)}.example.org`,
    `${"a.".repeat(130)}org`,
  ])("drops %j", (value) => {
    expect(loginHint(value)).toBeUndefined()
  })
})
