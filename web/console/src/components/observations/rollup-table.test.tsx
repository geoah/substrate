// @vitest-environment jsdom
/** The numbers under a chart: a row fed by a stream that names no source
 * lists only the sources that have a name. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import type { AggregateRow } from "@/lib/api/types"
import { WITHINGS } from "@/lib/observations.test-fixtures"

import { RollupTable } from "./rollup-table"

afterEach(cleanup)

describe("the rollup table", () => {
  it("names only the sources a row was fed by that have a name", () => {
    // The server answers "" for a stream that names no source.
    const row: AggregateRow = {
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      sources: ["", WITHINGS],
      values: [{ reducer: "avg", value: "75", unit: "kg" }],
      count: 2,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    }
    render(
      <RollupTable
        rows={[row]}
        reducers={["avg"]}
        groupCell={() => null}
        sourceText={(s) => `named ${s.slice(s.lastIndexOf("/") + 1)}`}
        label="Weight in numbers"
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Show the numbers" }))
    const cells = screen.getAllByRole("cell").map((c) => c.textContent)
    expect(cells).toContain("named withings-body-plus")
  })

  const day = (i: number, value: string, coverage?: string): AggregateRow => ({
    start: new Date(2026, 8, 18 + i).toISOString(),
    end: new Date(2026, 8, 19 + i).toISOString(),
    values: [{ reducer: "sum", value, unit: "kJ" }],
    count: 1,
    bounded: 0,
    synthetic: false,
    unavailable: false,
    ...(coverage ? { coverage } : {}),
  })

  function table(rows: AggregateRow[]) {
    render(
      <RollupTable
        rows={rows}
        reducers={["sum"]}
        groupCell={() => null}
        sourceText={(s) => s}
        label="Energy in numbers"
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Show the numbers" }))
    return screen.getByRole("table").parentElement!
  }

  it("groups a value's digits and gives the covered time to the minute", () => {
    table([day(0, "12008", "PT22H25M17S")])
    const cells = screen.getAllByRole("cell").map((c) => c.textContent)
    expect(cells).toContain("12,008 kJ")
    expect(cells).toContain("22h 25m")
  })

  it("says a stage's minutes in hours and minutes, under a head with no unit", () => {
    render(
      <RollupTable
        rows={[
          {
            ...day(0, "0"),
            values: [{ reducer: "sum", value: "257.73", unit: "min" }],
          },
        ]}
        reducers={["sum"]}
        groupCell={() => null}
        sourceText={(s) => s}
        label="Sleep stage in numbers"
        precision={2}
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Show the numbers" }))
    expect(screen.getAllByRole("cell").map((c) => c.textContent)).toContain(
      "4h 17m 44s"
    )
    expect(screen.getByRole("columnheader", { name: "Total" })).toBeTruthy()
  })

  it("holds every row of a short table, and scrolls a long one", () => {
    const ten = table(Array.from({ length: 10 }, (_, i) => day(i, "8000")))
    expect(ten.style.maxHeight).toBe("")
    cleanup()
    const many = table(Array.from({ length: 30 }, (_, i) => day(i, "8000")))
    expect(many.style.maxHeight).toBe("409px")
  })
})
