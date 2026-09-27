// @vitest-environment jsdom
/** The change detail band. Two things it must do: say which records the
 * write moved and where each stands (`affected`, decision 0061), and drop
 * nothing the wire said, so the whole payload stays one `raw` toggle away.
 * This test pins both. */

import { cleanup, render, screen } from "@testing-library/react"
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

/** A write that moved two records: the one it created and one it tombstoned
 * beside it, as the wire names them. */
const ROW: ChangeRow = {
  seq: 42,
  ts: "2026-08-12T01:10:32Z",
  actor: "providers.substrate.reamde.dev/github",
  op: "put",
  recordId: "p1",
  kind: "samples.substrate.reamde.dev/people/person",
  payload: {
    created: true,
    properties: ["name", "email"],
  },
  affected: [
    {
      kind: "samples.substrate.reamde.dev/people/person",
      id: "p1",
      version: 1,
    },
    {
      kind: "samples.substrate.reamde.dev/people/person",
      id: "p2",
      version: 4,
      deleted: true,
    },
  ],
}

afterEach(cleanup)

describe("ChangeDetail", () => {
  it("shows the raw payload without another disclosure", () => {
    render(<ChangeDetail row={ROW} />)
    expect(screen.getByText(/"properties"/)).toBeTruthy()
  })

  it("says which records the write moved and where each stands", () => {
    const { container } = render(<ChangeDetail row={ROW} />)
    expect(container.textContent).toContain("2 records")
    expect(container.textContent).toContain("person/p1")
    expect(container.textContent).toContain("version 1")
    expect(container.textContent).toContain("person/p2")
    expect(container.textContent).toContain("deleted")
  })

  it("attributes the write to its actor", () => {
    render(<ChangeDetail row={ROW} />)
    expect(screen.getByText("GitHub")).toBeTruthy()
  })

  it("keeps the raw payload expanded", () => {
    render(<ChangeDetail row={ROW} />)
    expect(screen.queryByText(/"properties"/)).toBeTruthy()
  })

  it("shows full source actors and distinguishes the mapping initiator", () => {
    const source = "function:providers.substrate.reamde.dev:google:synccontacts"
    const initiator = "function:example.com:people:promotecontact"
    render(
      <ChangeDetail
        row={{
          ...ROW,
          actor: "substrate",
          payload: {
            managers: { name: source },
            mechanism: "mapping",
            triggeredBy: initiator,
          },
        }}
      />
    )
    // Plain names on the line; each raw actor rides its hover card.
    expect(screen.getAllByText("Google Contacts sync").length).toBeGreaterThan(
      0
    )
    expect(screen.getByText("promotecontact")).toBeTruthy()
    expect(screen.getByText("Mapping recomputation by the engine")).toBeTruthy()
    expect(screen.getByText("committed by")).toBeTruthy()
    expect(screen.getByText("Substrate")).toBeTruthy()
  })

  it("renders a row that names no records without a records section", () => {
    const { container } = render(
      <ChangeDetail
        row={{ ...ROW, affected: undefined, payload: { properties: ["name"] } }}
      />
    )
    expect(container.textContent).toContain("property")
    expect(container.textContent).not.toContain("1 record")
  })
})
