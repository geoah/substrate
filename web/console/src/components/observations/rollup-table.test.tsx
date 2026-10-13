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
})
