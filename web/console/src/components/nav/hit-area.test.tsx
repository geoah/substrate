// @vitest-environment jsdom
/** Small controls keep their look and take a 24px hit area (the `hit-area`
 * utility in index.css). */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { CopyButton } from "@/components/identity/copy-button"
import { ToggleSwitch } from "@/components/nav/toggle-switch"

afterEach(cleanup)

describe("small controls", () => {
  it("grow the copy button's hit area, not its glyph", () => {
    render(<CopyButton value="task-1" label="Copy record id" />)
    const button = screen.getByRole("button", { name: "Copy record id" })
    expect(button.className).toContain("hit-area")
    expect(button.className).toContain("size-5")
  })

  it("grow the switch's hit area, not the switch", () => {
    render(
      <ToggleSwitch checked={false} onChange={() => {}} label="Technical" />
    )
    expect(
      screen.getByRole("switch", { name: "Technical" }).className
    ).toContain("hit-area")
  })
})
