// @vitest-environment jsdom
/** A reply's links: a web page opens outside the console with no opener and
 * no referrer, a record link is the record, and no other target becomes an
 * address. */

import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => (
    <a data-record-ref="">{children}</a>
  ),
}))

import { MessageText } from "./message-text"

afterEach(cleanup)

describe("MessageText links", () => {
  it("opens a web link outside the console", () => {
    render(
      <MessageText text="See [BBC Weather](https://www.bbc.com/weather/2761369)." />
    )
    const link = screen.getByRole("link", { name: "BBC Weather" })
    expect(link.getAttribute("href")).toBe(
      "https://www.bbc.com/weather/2761369"
    )
    expect(link.getAttribute("target")).toBe("_blank")
    expect(link.getAttribute("rel")).toContain("noopener")
    expect(link.getAttribute("rel")).toContain("noreferrer")
  })

  it("renders a record link as the record, not its markdown", () => {
    const { container } = render(
      <MessageText text="[Skittle Lane](substrate://mneme.rxtx.dev/coffee/bean/skittle) is saved" />
    )
    expect(container.querySelector("[data-record-ref]")).toBeTruthy()
    expect(container.textContent).not.toContain("substrate://")
    expect(container.textContent).not.toContain("](")
  })

  it("gives no other target an address", () => {
    const { container } = render(
      <MessageText text="[click](javascript:alert(1))" />
    )
    expect(container.querySelector("a")).toBeNull()
    expect(container.textContent).toBe("click (javascript:alert(1))")
  })
})
