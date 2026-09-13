// @vitest-environment jsdom
/** `Screen` drives the host's chrome through the SDK: the title on mount and
 * on change, the primary button set while one is declared and cleared when
 * it goes, one click listener however often the closure changes, and the
 * back button wired to `host.back` only when asked. */

import { cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const sdk = vi.hoisted(() => {
  const primaryClicks: (() => void)[] = []
  const backClicks: (() => void)[] = []
  return {
    primaryClicks,
    backClicks,
    host: {
      title: vi.fn(),
      back: vi.fn(),
      navigate: vi.fn(() => Promise.resolve()),
      openLink: vi.fn(() => Promise.resolve()),
      toast: vi.fn(),
      primaryAction: {
        set: vi.fn(),
        onClick: vi.fn((cb: () => void) => {
          primaryClicks.push(cb)
          return () => {
            primaryClicks.splice(primaryClicks.indexOf(cb), 1)
          }
        }),
      },
      backButton: {
        onClick: vi.fn((cb: () => void) => {
          backClicks.push(cb)
          return () => {
            backClicks.splice(backClicks.indexOf(cb), 1)
          }
        }),
      },
    },
    records: {},
    useKind: () => undefined,
  }
})
vi.mock("./sdk", () => sdk)

import { Screen } from "./screen"

describe("Screen", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sdk.primaryClicks.length = 0
    sdk.backClicks.length = 0
  })
  afterEach(cleanup)

  it("sets the title, and again when it changes", () => {
    const { rerender } = render(<Screen title="Tasks" />)
    expect(sdk.host.title).toHaveBeenCalledWith("Tasks")
    rerender(<Screen title="Website" />)
    expect(sdk.host.title).toHaveBeenLastCalledWith("Website")
    expect(sdk.host.title).toHaveBeenCalledTimes(2)
  })

  it("sets the primary button, routes its click to the latest onPress, and clears it on unmount", () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender, unmount } = render(
      <Screen title="T" primary={{ label: "Add", onPress: first }} />
    )
    expect(sdk.host.primaryAction.set).toHaveBeenCalledWith({
      label: "Add",
      enabled: true,
    })
    expect(sdk.primaryClicks).toHaveLength(1)
    sdk.primaryClicks[0]()
    expect(first).toHaveBeenCalledTimes(1)

    rerender(
      <Screen
        title="T"
        primary={{ label: "Add", onPress: second, enabled: false }}
      />
    )
    expect(sdk.host.primaryAction.set).toHaveBeenLastCalledWith({
      label: "Add",
      enabled: false,
    })
    expect(sdk.primaryClicks).toHaveLength(1)
    sdk.primaryClicks[0]()
    expect(second).toHaveBeenCalledTimes(1)
    expect(first).toHaveBeenCalledTimes(1)

    unmount()
    expect(sdk.host.primaryAction.set).toHaveBeenLastCalledWith(null)
    expect(sdk.primaryClicks).toHaveLength(0)
  })

  it("leaves the primary button alone when none is declared", () => {
    render(<Screen title="T" />)
    expect(sdk.host.primaryAction.set).not.toHaveBeenCalled()
    expect(sdk.host.primaryAction.onClick).not.toHaveBeenCalled()
  })

  it("wires the back button to host.back only with `back`", () => {
    const { unmount } = render(<Screen title="T" />)
    expect(sdk.host.backButton.onClick).not.toHaveBeenCalled()
    unmount()
    const mounted = render(<Screen title="T" back />)
    expect(sdk.backClicks).toHaveLength(1)
    sdk.backClicks[0]()
    expect(sdk.host.back).toHaveBeenCalledTimes(1)
    mounted.unmount()
    expect(sdk.backClicks).toHaveLength(0)
  })

  it("draws a loading line while loading", () => {
    const { container, rerender } = render(<Screen title="T" loading />)
    expect(container.querySelector(".kit-progress")).not.toBeNull()
    rerender(<Screen title="T" />)
    expect(container.querySelector(".kit-progress")).toBeNull()
  })
})
