// @vitest-environment jsdom
/** One failing part of the console never blanks the rest: a page that throws
 * while rendering says so in words and offers to try again, and a section
 * wrapped in its own boundary fails alone while its siblings stay. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>()
  return {
    ...actual,
    Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    useRouter: () => ({ invalidate: () => Promise.resolve() }),
  }
})

import { PageError, SectionBoundary } from "./page-error"

afterEach(cleanup)

function Boom(): ReactNode {
  throw new Error("definition.purpose is not iterable")
}

describe("PageError", () => {
  it("says the page failed, with the reason, and tries again", () => {
    const reset = vi.fn()
    render(<PageError error={new Error("kaboom")} reset={reset} />)
    expect(screen.getByText("This page couldn’t be shown")).toBeTruthy()
    // An error message is prose, never the monospace voice of an id.
    expect(screen.getByText(/kaboom/).className).not.toMatch(/font-mono/)
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(reset).toHaveBeenCalled()
  })
})

describe("SectionBoundary", () => {
  it("fails one section and keeps its siblings", () => {
    const quiet = vi.spyOn(console, "error").mockImplementation(() => {})
    render(
      <div>
        <SectionBoundary name="The sidebar">
          <Boom />
        </SectionBoundary>
        <p>The page itself</p>
      </div>
    )
    quiet.mockRestore()
    expect(screen.getByText("The page itself")).toBeTruthy()
    expect(screen.getByRole("alert").textContent).toContain(
      "The sidebar couldn’t be shown"
    )
  })
})
