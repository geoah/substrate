import { describe, expect, it, vi } from "vitest"
import app from "../../app.json"

vi.stubGlobal("__APP_VERSION__", "0.0.0-test")
const { PACKAGES, SCOPES } = await import("./manifest")

describe("the manifest", () => {
  it("asks for app.json's packages at its versions, in its order", () => {
    expect(PACKAGES.map(([p, v]) => ({ package: p, atLeast: v }))).toEqual(
      app.asks
    )
  })

  it("asks for app.json's scopes, in its order", () => {
    expect(SCOPES).toEqual(app.scopes)
  })
})
