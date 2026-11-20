import { describe, expect, it } from "vitest"
import { parseRoute, routeHash, type Route } from "./route"

const routes: Route[] = [
  { screen: "today" },
  { screen: "labs", query: "" },
  { screen: "labs", query: "status=out&range=2y" },
  { screen: "lab", entry: "ldl", query: "" },
  { screen: "reports" },
  { screen: "report", id: "abc/report/1" },
  { screen: "healthspan", query: "" },
  { screen: "healthspanMetric", id: "vo2max" },
  { screen: "how" },
  { screen: "record", query: "provider=gp" },
  { screen: "sources" },
  { screen: "source", id: "applehealth/phone/apple-watch" },
  { screen: "import" },
  { screen: "import", id: "0193" },
  { screen: "settings" },
  { screen: "start" },
]

describe("routes", () => {
  it.each(routes)("round-trips %o through the hash", (route) => {
    expect(parseRoute(routeHash(route))).toEqual(route)
  })

  it("opens Today for an empty or unknown hash", () => {
    expect(parseRoute("")).toEqual({ screen: "today" })
    expect(parseRoute("#/nowhere")).toEqual({ screen: "today" })
  })
})
