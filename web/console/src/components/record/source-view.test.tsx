// @vitest-environment jsdom
/** The source view: the YAML envelope by default, carrying the sources a
 * mapping links here, and the JSON the API serves one press away, copyable. */

import type { ReactNode } from "react"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...rest }: { children?: ReactNode }) => (
    <a href="#" {...(rest as object)}>
      {children}
    </a>
  ),
}))

import { SourceView } from "./source-view"
import type { SubstrateRecord } from "@/lib/api/types"

const record: SubstrateRecord = {
  id: "grace",
  kind: "ada.example.com/people/person",
  properties: { name: "Grace" },
  labels: {},
  version: 2,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
  linkedFrom: [
    {
      ref: "providers.substrate.reamde.dev/google/contact/c1",
      kind: "providers.substrate.reamde.dev/google/contact",
      property: "person",
      mapping: "ada.example.com/people/googlecontactperson",
    },
  ],
}

afterEach(cleanup)

describe("SourceView", () => {
  it("shows the YAML envelope, linkedFrom included", () => {
    render(<SourceView record={record} kinds={[]} />)
    const view = document.querySelector("[data-slot=record-source]")!
    expect(view.textContent).toContain("linkedFrom")
    expect(view.textContent).toContain("googlecontactperson")
    expect(
      screen.getByRole("radio", { name: "YAML" }).getAttribute("aria-checked")
    ).toBe("true")
  })

  it("switches to the JSON the API serves, with a copy button", () => {
    render(<SourceView record={record} kinds={[]} />)
    fireEvent.click(screen.getByRole("radio", { name: "JSON" }))
    const view = document.querySelector("[data-slot=record-source]")!
    expect(view.textContent).toContain('"id": "grace"')
    expect(view.textContent).toContain('"linkedFrom": [')
    expect(
      screen.getByRole("button", { name: "Copy the record’s JSON" })
    ).toBeTruthy()
  })
})
