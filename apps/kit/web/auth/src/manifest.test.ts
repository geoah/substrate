import { describe, expect, it } from "vitest"
import { appBase } from "./manifest"

describe("appBase", () => {
  it("is the page's folder, with or without its slash", () => {
    const at = (url: string) => appBase(new URL(url)).toString()
    expect(at("https://x.example/substrate/tasks/")).toBe(
      "https://x.example/substrate/tasks/"
    )
    expect(at("https://x.example/substrate/tasks")).toBe(
      "https://x.example/substrate/tasks/"
    )
    expect(at("https://x.example/substrate/tasks/index.html")).toBe(
      "https://x.example/substrate/tasks/"
    )
    expect(at("https://x.example/?code=c#/item/a")).toBe("https://x.example/")
  })
})
