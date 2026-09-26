// @vitest-environment jsdom
/** The heads' promises: a section heading is one level-2 heading that names
 * its section, with its hint and actions beside it; a page head keeps one
 * place for the glyph, the title, the meta line and the actions. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { SectionHead } from "./section-head"

afterEach(cleanup)

describe("SectionHead", () => {
  it("is a level-2 heading carrying its id, with the hint and actions beside it", () => {
    render(
      <section aria-labelledby="brings-in">
        <SectionHead
          id="brings-in"
          title="What it brings in"
          hint="4 collections"
          actions={<button type="button">Add</button>}
        />
      </section>
    )
    const heading = screen.getByRole("heading", {
      level: 2,
      name: "What it brings in",
    })
    expect(heading.id).toBe("brings-in")
    expect(
      screen.getByRole("region", { name: "What it brings in" })
    ).toBeTruthy()
    expect(screen.getByText("4 collections")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy()
  })

  it("draws no hint or actions it was not given", () => {
    const { container } = render(<SectionHead title="History" />)
    const head = container.querySelector('[data-slot="section-head"]')!
    expect(head.children).toHaveLength(1)
  })
})
