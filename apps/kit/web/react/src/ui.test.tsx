import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { Alert, LargeTitle, NavBar } from "./ui"

afterEach(cleanup)

describe("the screen pieces", () => {
  it("give a screen one h1: the large title where there is one, else the bar's title", () => {
    const { unmount } = render(
      <>
        <NavBar title="Items" heading={false} />
        <LargeTitle>Items</LargeTitle>
      </>
    )
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1)
    expect(screen.getByRole("heading", { level: 1 }).className).toMatch(
      /text-\[34px\]/
    )
    unmount()
    render(<NavBar title="Item" />)
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Item")
  })

  it("draws every answer of an alert, two with the same words included", () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {})
    render(
      <Alert
        open
        title="Which one?"
        onCancel={() => {}}
        buttons={[
          { label: "Keep", onClick: () => {} },
          { label: "Keep", role: "cancel", onClick: () => {} },
        ]}
      />
    )
    expect(screen.getAllByRole("button", { name: "Keep" })).toHaveLength(2)
    expect(errors).not.toHaveBeenCalled()
  })
})
