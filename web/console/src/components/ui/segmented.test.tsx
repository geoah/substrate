// @vitest-environment jsdom
/** A segmented control is a radio group: one tab stop, the arrows and
 * Home/End move the choice, a click chooses. */

import { useState } from "react"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Segmented } from "./segmented"

afterEach(cleanup)

const OPTIONS = [
  { value: "a", label: "Alpha" },
  { value: "b", label: "Beta" },
  { value: "c", label: "Gamma" },
] as const

function Harness({ onChange }: { onChange?: (v: string) => void }) {
  const [value, setValue] = useState<"a" | "b" | "c">("a")
  return (
    <Segmented
      label="Letter"
      value={value}
      options={OPTIONS}
      onChange={(v) => {
        setValue(v)
        onChange?.(v)
      }}
    />
  )
}

const radio = (name: string) => screen.getByRole("radio", { name })

describe("Segmented", () => {
  it("is a named radio group with one tab stop, on the choice", () => {
    render(<Harness />)
    expect(screen.getByRole("radiogroup", { name: "Letter" })).toBeTruthy()
    expect(radio("Alpha").getAttribute("aria-checked")).toBe("true")
    expect(radio("Alpha").tabIndex).toBe(0)
    expect(radio("Beta").tabIndex).toBe(-1)
    expect(radio("Gamma").tabIndex).toBe(-1)
  })

  it("moves the choice and the focus with the arrows, wrapping", () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    radio("Alpha").focus()
    fireEvent.keyDown(radio("Alpha"), { key: "ArrowRight" })
    expect(onChange).toHaveBeenLastCalledWith("b")
    expect(document.activeElement).toBe(radio("Beta"))
    expect(radio("Beta").tabIndex).toBe(0)
    fireEvent.keyDown(radio("Beta"), { key: "ArrowDown" })
    fireEvent.keyDown(radio("Gamma"), { key: "ArrowRight" })
    expect(onChange).toHaveBeenLastCalledWith("a")
    fireEvent.keyDown(radio("Alpha"), { key: "ArrowLeft" })
    expect(onChange).toHaveBeenLastCalledWith("c")
    expect(document.activeElement).toBe(radio("Gamma"))
  })

  it("jumps to the ends with Home and End", () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    fireEvent.keyDown(radio("Alpha"), { key: "End" })
    expect(onChange).toHaveBeenLastCalledWith("c")
    fireEvent.keyDown(radio("Gamma"), { key: "Home" })
    expect(onChange).toHaveBeenLastCalledWith("a")
    expect(document.activeElement).toBe(radio("Alpha"))
  })

  it("chooses on a click and ignores other keys", () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    fireEvent.keyDown(radio("Alpha"), { key: "x" })
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(radio("Gamma"))
    expect(onChange).toHaveBeenLastCalledWith("c")
    expect(radio("Gamma").getAttribute("aria-checked")).toBe("true")
  })
})
