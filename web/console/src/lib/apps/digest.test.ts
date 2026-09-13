/** The mount key: the same code hashes the same whatever order the modules
 * arrive in, and any change to the code moves it. */

import { describe, expect, it } from "vitest"

import { digest } from "./digest"

describe("digest", () => {
  it("is stable across module order and moves with the code", async () => {
    const a = await digest("export default 1", { b: "2", a: "3" })
    const b = await digest("export default 1", { a: "3", b: "2" })
    expect(a).toBe(b)
    expect(a).toMatch(/^[0-9a-f]{64}$/)
    expect(await digest("export default 2", { a: "3", b: "2" })).not.toBe(a)
    expect(await digest("export default 1", { a: "3" })).not.toBe(a)
    expect(await digest("export default 1")).toBe(
      await digest("export default 1", {})
    )
  })
})
