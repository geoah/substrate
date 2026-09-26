// @vitest-environment jsdom
/** The heads' promises: a section heading is one level-2 heading that names
 * its section, with its hint and actions beside it; a page head keeps one
 * place for the glyph, the title, the meta line and the actions. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { PageHeader } from "./page-header"
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

describe("PageHeader", () => {
  it("sets the glyph above the title in the record layout, on one row with the actions", () => {
    const { container } = render(
      <PageHeader
        size="record"
        glyph={<span data-testid="glyph" />}
        actions={<button type="button">More</button>}
        title="Buy milk"
        meta="Added 2 days ago"
      />
    )
    const header = container.querySelector('[data-slot="page-header"]')!
    expect(header.getAttribute("data-layout")).toBe("record")
    const row = header.firstElementChild!
    expect(row.contains(screen.getByTestId("glyph"))).toBe(true)
    expect(row.contains(screen.getByRole("button", { name: "More" }))).toBe(
      true
    )
    expect(row.contains(screen.getByRole("heading", { level: 1 }))).toBe(false)
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Buy milk"
    )
    expect(screen.getByText("Added 2 days ago")).toBeTruthy()
  })

  it("draws a page's own heading in place of the h1 it would draw", () => {
    render(
      <PageHeader
        size="record"
        heading={<input aria-label="Name" defaultValue="Buy milk" />}
        title="ignored"
      />
    )
    expect(screen.queryByRole("heading")).toBeNull()
    expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy()
  })

  it("drops the actions under the title on a phone, never beside a squeezed title", () => {
    const { container } = render(
      <PageHeader
        title="Google Contacts sync"
        actions={<button type="button">Pause</button>}
      />
    )
    const actions = container.querySelector('[data-slot="page-actions"]')!
    // Under 560px the actions take a line of their own; from 560px up they
    // sit beside the title again.
    expect(actions.parentElement!.className).toContain("basis-full")
    expect(actions.parentElement!.className).toContain("min-[560px]:basis-auto")
    const title = screen.getByRole("heading", { level: 1 })
    expect(title.className).not.toContain("break-all")
    expect(title.className).not.toContain("overflow-wrap:anywhere")
  })
})
