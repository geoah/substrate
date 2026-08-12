import { describe, expect, it } from "vitest"

import { columnIdOf, propertyColumnId, sortPropertyOf } from "./kind-browse-columns"

describe("column id ↔ wire property mapping", () => {
  it("namespaces declared properties apart from system columns", () => {
    // pullrequest declares its own `updatedAt`; the system column must
    // not collide with it (live finding, 2026-08-05).
    expect(propertyColumnId("updatedAt")).toBe("prop:updatedAt")
    expect(sortPropertyOf("prop:number")).toBe("number")
    expect(sortPropertyOf("updatedAt")).toBe("updatedAt")
  })

  it("routes a wire sort property back to the owning column", () => {
    expect(columnIdOf("updatedAt")).toBe("updatedAt")
    expect(columnIdOf("title")).toBe("title")
    expect(columnIdOf("prominence")).toBe("prop:prominence")
  })
})
