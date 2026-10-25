// @vitest-environment jsdom
/** Page setup shows the size the note has, whatever changed it: an undo
 * outside the popover moves the chosen paper and the custom numbers with it. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { INK_PAGE_SIZES, type InkSize } from "@/lib/ink"

import { PageSetup } from "./page-setup"

afterEach(cleanup)

function setup(size: InkSize) {
  const view = (at: InkSize) => (
    <PageSetup
      size={at}
      onSize={() => undefined}
      background="lined"
      onBackground={() => undefined}
      pages={1}
      inkOutside={false}
    />
  )
  const { rerender } = render(view(size))
  return { resize: (next: InkSize) => rerender(view(next)) }
}

const checked = (name: string) =>
  screen.getByRole("radio", { name }).getAttribute("aria-checked")

describe("PageSetup", () => {
  it("follows the size when it changes from outside", async () => {
    const { resize } = setup({ width: 1000, height: 1000 })
    fireEvent.click(screen.getByRole("button", { name: /Page setup/ }))
    await screen.findByRole("radio", { name: "Custom" })
    expect(checked("Custom")).toBe("true")

    resize(INK_PAGE_SIZES.a4)
    await waitFor(() => expect(checked("A4")).toBe("true"))
    expect(checked("Custom")).toBe("false")
  })

  it("keeps Custom open on a preset size, with the numbers the size has", async () => {
    const { resize } = setup(INK_PAGE_SIZES.a4)
    fireEvent.click(screen.getByRole("button", { name: /Page setup/ }))
    fireEvent.click(await screen.findByRole("radio", { name: "Custom" }))
    const width = () =>
      screen.getByRole<HTMLInputElement>("spinbutton", { name: "Width" })
    expect(width().value).toBe("1240")

    resize({ width: 900, height: INK_PAGE_SIZES.a4.height })
    await waitFor(() => expect(width().value).toBe("900"))
    expect(checked("Custom")).toBe("true")
  })
})
