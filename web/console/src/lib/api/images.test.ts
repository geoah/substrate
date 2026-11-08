// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"

import { proxied, proxiedImage } from "./images"

describe("the image proxy client", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("sends another origin's image through the proxy and draws this one's as it is", () => {
    expect(proxied("https://covers.example.com/b/1-M.jpg")).toBe(true)
    expect(proxied("/assets/logo.png")).toBe(false)
    expect(proxied("data:image/png;base64,AAAA")).toBe(false)
  })

  it("fetches through the proxy once per address and answers a blob URL", async () => {
    const fetch = vi.fn(
      async () => new Response(new Blob(["png"]), { status: 200 })
    )
    vi.stubGlobal("fetch", fetch)
    const createObjectURL = vi.fn(() => "blob:cover")
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL }))
    const url = "https://covers.example.com/b/2-M.jpg"
    expect(await proxiedImage(url)).toBe("blob:cover")
    expect(await proxiedImage(url)).toBe("blob:cover")
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith(
      `/api/v1/images?url=${encodeURIComponent(url)}`,
      expect.anything()
    )
  })

  it("draws nothing for an http address or a refusal", async () => {
    const fetch = vi.fn(async () => new Response("", { status: 404 }))
    vi.stubGlobal("fetch", fetch)
    expect(
      await proxiedImage("http://covers.example.com/3.jpg")
    ).toBeUndefined()
    expect(
      await proxiedImage("https://covers.example.com/4.jpg")
    ).toBeUndefined()
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
