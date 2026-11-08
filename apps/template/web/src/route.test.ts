import { describe, expect, it } from "vitest"
import { parseRoute, routeHash, type Route } from "./route"

describe("the template's routes", () => {
  it("round-trip through the hash", () => {
    const routes: Route[] = [
      { screen: "items" },
      { screen: "item", id: "0190a1b2-0000-7000-8000-000000000001" },
      { screen: "settings" },
    ]
    for (const route of routes)
      expect(parseRoute(routeHash(route))).toEqual(route)
    expect(parseRoute("#/nowhere")).toEqual({ screen: "items" })
  })
})
