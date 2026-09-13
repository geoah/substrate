// @vitest-environment jsdom
/** `Row`: a tap opens the record through the host unless `onTap` says
 * otherwise, `when` reads relative, the first action is a trailing button
 * that opens its link through the host, and the rest wait behind a hold. */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const sdk = vi.hoisted(() => ({
  host: {
    navigate: vi.fn(() => Promise.resolve()),
    openLink: vi.fn(() => Promise.resolve()),
    toast: vi.fn(),
  },
  records: {},
  useKind: () => undefined,
}))
vi.mock("./sdk", () => sdk)

import { Row } from "./row"

const person: SubstrateRecord = {
  id: "ada",
  kind: "ada.example.com/people/person",
  properties: { name: "Ada" },
  labels: {},
  version: 3,
  createdAt: "",
  updatedAt: "",
}

const DAY = 86_400_000

describe("Row", () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it("opens the record through the host on tap", () => {
    render(<Row title="Ada" record={person} />)
    fireEvent.click(screen.getByRole("button", { name: "Ada" }))
    expect(sdk.host.navigate).toHaveBeenCalledWith({
      record: { kind: person.kind, id: "ada" },
    })
  })

  it("prefers onTap, and answers Enter", () => {
    const onTap = vi.fn()
    render(<Row title="Ada" record={person} onTap={onTap} chevron />)
    const button = screen.getByRole("button", { name: "Ada" })
    fireEvent.click(button)
    fireEvent.keyDown(button, { key: "Enter" })
    expect(onTap).toHaveBeenCalledTimes(2)
    expect(sdk.host.navigate).not.toHaveBeenCalled()
  })

  it("is not a button without a tap, and shows subtitle and meta", () => {
    render(<Row title="Ada" subtitle="ada@example.com" meta="#12 · ada" />)
    expect(screen.queryByRole("button")).toBeNull()
    expect(screen.getByText("ada@example.com").className).toBe(
      "kit-row-subtitle"
    )
    expect(screen.getByText("#12 · ada").className).toBe("kit-row-meta")
  })

  it("reads `when` relative and marks the past", () => {
    const tomorrow = new Date(Date.now() + DAY).toISOString()
    const { rerender, container } = render(<Row title="T" when={tomorrow} />)
    expect(screen.getByText("tomorrow").className).toBe("kit-row-when")
    rerender(<Row title="T" when={new Date(Date.now() - DAY).toISOString()} />)
    expect(container.querySelector(".kit-row-when--past")?.textContent).toBe(
      "yesterday"
    )
  })

  it("draws the first action as a trailing button that opens through the host", () => {
    render(
      <Row
        title="Ada"
        actions={[
          undefined,
          { icon: "mail", href: "mailto:ada@example.com" },
          false,
          { icon: "phone", href: "tel:+441234" },
        ]}
      />
    )
    const mail = screen.getByRole("button", { name: "Mail" })
    expect(mail.className).toContain("kit-icon-btn")
    fireEvent.click(mail)
    expect(sdk.host.openLink).toHaveBeenCalledWith("mailto:ada@example.com")
    expect(screen.queryByRole("button", { name: "Phone" })).toBeNull()
  })

  it("opens the rest under a hold, and the lift is not a tap", () => {
    vi.useFakeTimers()
    const onPress = vi.fn()
    render(
      <Row
        title="Ada"
        record={person}
        actions={[
          { icon: "mail", href: "mailto:ada@example.com" },
          { icon: "phone", href: "tel:+441234" },
          { icon: "star", label: "Favourite", onPress },
        ]}
      />
    )
    const main = screen.getByRole("button", { name: "Ada" })
    fireEvent.pointerDown(main, { button: 0, clientX: 10, clientY: 10 })
    act(() => {
      vi.advanceTimersByTime(500)
    })
    fireEvent.pointerUp(main)
    fireEvent.click(main)
    expect(sdk.host.navigate).not.toHaveBeenCalled()
    const menu = screen.getByRole("menu", { name: "More actions" })
    expect(menu).toBeTruthy()
    expect(screen.queryByRole("menuitem", { name: "Mail" })).toBeNull()
    fireEvent.click(screen.getByRole("menuitem", { name: "Favourite" }))
    expect(onPress).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("menu")).toBeNull()
    fireEvent.contextMenu(main)
    fireEvent.click(screen.getByRole("menuitem", { name: "Phone" }))
    expect(sdk.host.openLink).toHaveBeenCalledWith("tel:+441234")
  })

  it("cancels the hold when the finger moves or lifts early", () => {
    vi.useFakeTimers()
    render(
      <Row
        title="Ada"
        record={person}
        actions={[{ icon: "mail" }, { icon: "phone", href: "tel:1" }]}
      />
    )
    const main = screen.getByRole("button", { name: "Ada" })
    fireEvent.pointerDown(main, { button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(main, { clientX: 40, clientY: 10 })
    act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(screen.queryByRole("menu")).toBeNull()
    fireEvent.pointerDown(main, { button: 0, clientX: 10, clientY: 10 })
    act(() => {
      vi.advanceTimersByTime(200)
    })
    fireEvent.pointerUp(main)
    fireEvent.click(main)
    act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(screen.queryByRole("menu")).toBeNull()
    expect(sdk.host.navigate).toHaveBeenCalledTimes(1)
  })
})
