/** The change detail band. Two things it must never do: leak the INTERNAL name
 * of the mechanism that records a write's effects, and drop something the wire
 * said. So the effects render as English, the whole payload stays one `raw`
 * toggle away, and this test pins both. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { ChangeRow } from "@/lib/api/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    title,
    children,
  }: {
    to: string
    title?: string
    children: React.ReactNode
  }) => (
    <a href={to} title={title}>
      {children}
    </a>
  ),
}))

import { ChangeDetail } from "./row-detail"

/** A put whose payload carries the recorded effects under the engine's own key
 * — the wire shape, spelled here because this is the wire. */
const ROW: ChangeRow = {
  seq: 42,
  ts: "2026-08-12T01:10:32Z",
  actor: "github.bundles.substrate.reamde.dev",
  op: "put",
  recordId: "p1",
  kind: "people.substrate.geoah.me/person",
  payload: {
    created: true,
    properties: ["name", "email"],
    fold: [
      {
        kind: "record",
        ref: "people.substrate.geoah.me/person",
        id: "p1",
        delta: { created: true, set: { name: "Ada" }, del: ["nickname"] },
      },
      {
        kind: "edge",
        ref: "people.substrate.geoah.me/person",
        id: "p1",
        rel: "member",
        dstType: "people.substrate.geoah.me/organization",
        dst: "o1",
      },
    ],
  },
}

afterEach(cleanup)

describe("ChangeDetail", () => {
  it("never shows the mechanism's name, open or closed", () => {
    const { container } = render(<ChangeDetail row={ROW} />)
    expect(container.textContent).not.toContain("fold")
    fireEvent.click(screen.getByRole("button", { name: /raw/i }))
    // Open, the raw JSON is the payload verbatim — the key is IN the JSON, and
    // that is the point: the disclosure is the one place it may appear.
    expect(screen.getByText(/"fold"/)).toBeTruthy()
  })

  it("says what the write did, per effect, in English", () => {
    const { container } = render(<ChangeDetail row={ROW} />)
    expect(container.textContent).toContain("2 changes")
    expect(container.textContent).toContain("created")
    expect(container.textContent).toContain("set name; cleared nickname")
    expect(container.textContent).toContain("linked")
    expect(container.textContent).toContain(
      "member → people.substrate.geoah.me/organization/o1"
    )
  })

  it("attributes the write to its actor", () => {
    render(<ChangeDetail row={ROW} />)
    expect(screen.getByTitle("github.bundles.substrate.reamde.dev")).toBeTruthy()
  })

  it("keeps the raw payload closed until asked", () => {
    render(<ChangeDetail row={ROW} />)
    expect(screen.queryByText(/"fold"/)).toBeNull()
  })

  it("renders a row with no recorded effects without an effects section", () => {
    const { container } = render(
      <ChangeDetail row={{ ...ROW, payload: { properties: ["name"] } }} />
    )
    expect(container.textContent).toContain("property")
    expect(container.textContent).not.toContain("1 change")
  })
})
