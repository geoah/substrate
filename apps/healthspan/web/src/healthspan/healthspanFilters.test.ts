import { describe, expect, it } from "vitest"
import type { HealthspanCardData } from "./healthspanCards"
import {
  applyHealthspanFilters,
  cardHref,
  DEFAULT_HEALTHSPAN_FILTERS,
  healthspanFilterCounts,
  healthspanFiltersToParams,
  parseHealthspanFilters,
} from "./healthspanFilters"
import type { HealthspanStatus } from "../rules/healthspan"

const c = (
  id: string,
  group: HealthspanCardData["group"],
  statuses: HealthspanStatus[]
) => ({ id, group, statuses: new Set(statuses) }) as HealthspanCardData

const cards = [
  c("a", "sleep", ["slipping"]),
  c("b", "strain", ["adds", "slipping"]),
  c("c", "fitness", ["adds"]),
  c("d", "signals", ["flagged"]),
  c("e", "signals", ["ok"]),
  c("f", "body", ["ok"]),
  c("g", "fitness", ["flagged", "nodata"]),
]

describe("Healthspan filters", () => {
  it("round-trip through the URL in a fixed order, defaults omitted", () => {
    const f = parseHealthspanFilters(
      new URLSearchParams("status=slipping,adds,bogus&group=body,sleep")
    )
    expect(f).toEqual({
      status: ["adds", "slipping"],
      group: ["sleep", "body"],
    })
    expect(healthspanFiltersToParams(f).toString()).toBe(
      "status=adds%2Cslipping&group=sleep%2Cbody"
    )
    expect(
      healthspanFiltersToParams(DEFAULT_HEALTHSPAN_FILTERS).toString()
    ).toBe("")
    expect(parseHealthspanFilters(new URLSearchParams(""))).toEqual(
      DEFAULT_HEALTHSPAN_FILTERS
    )
  })
  it("OR within a group and AND across groups", () => {
    const ids = (
      s: HealthspanStatus[],
      g: HealthspanCardData["group"][] = []
    ) => applyHealthspanFilters(cards, { status: s, group: g }).map((x) => x.id)
    expect(ids([])).toHaveLength(7)
    expect(ids(["adds", "slipping"])).toEqual(["a", "b", "c"])
    expect(ids(["flagged"], ["fitness"])).toEqual(["g"])
    expect(ids([], ["signals"])).toEqual(["d", "e"])
    expect(ids(["ok"], ["sleep"])).toEqual([])
  })
  it("counts one group among the cards passing the other", () => {
    const n = healthspanFilterCounts(cards, {
      status: ["adds"],
      group: ["signals"],
    })
    expect(n.status).toEqual({
      adds: 0,
      slipping: 0,
      flagged: 1,
      nodata: 0,
      ok: 1,
    })
    expect(n.group).toEqual({
      sleep: 0,
      strain: 1,
      fitness: 1,
      signals: 0,
      body: 0,
    })
  })
  it("opens a card from a link with the status filter dropped", () => {
    expect(cardHref(cards[1], { status: ["flagged"], group: [] })).toBe(
      "#/healthspan?focus=b"
    )
    expect(cardHref(cards[1], { status: [], group: ["strain", "sleep"] })).toBe(
      "#/healthspan?group=sleep%2Cstrain&focus=b"
    )
    expect(cardHref(cards[1], { status: [], group: ["body"] })).toBe(
      "#/healthspan?focus=b"
    )
  })
})
