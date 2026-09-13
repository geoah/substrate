// @vitest-environment jsdom
/** The strip names the place: a transform error reads `source:14:8` with
 * the author's line under it and a caret at the column; a module's error
 * names the module; a grant refusal and a provenance notice carry no line. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { ErrorsStrip, locationOf, sourceLine } from "./errors"

const SOURCE = Array.from(
  { length: 20 },
  (_, i) => `line ${i + 1}: const x${i + 1} = ${i + 1}`
).join("\n")

afterEach(cleanup)

describe("locationOf and sourceLine", () => {
  it("spells module:line:column and reads the line back", () => {
    const err = {
      phase: "transform" as const,
      message: "Unexpected token",
      line: 14,
      column: 8,
    }
    expect(locationOf(err)).toBe("source:14:8")
    expect(sourceLine(err, SOURCE, {})).toBe("line 14: const x14 = 14")
    const inModule = { ...err, module: "rows", line: 2 }
    expect(locationOf(inModule)).toBe("rows:2:8")
    expect(sourceLine(inModule, SOURCE, { rows: "a\nb\nc" })).toBe("b")
    expect(locationOf({ phase: "grant", message: "no" })).toBeUndefined()
  })
})

describe("ErrorsStrip", () => {
  it("renders nothing without errors", () => {
    const { container } = render(
      <ErrorsStrip errors={[]} source={SOURCE} modules={{}} />
    )
    expect(container.firstChild).toBeNull()
  })

  it("folds to the latest error and opens to the line with a caret", () => {
    render(
      <ErrorsStrip
        errors={[
          {
            phase: "grant",
            message: "permissions.reads.kinds: add ada.example.com/tasks/task",
          },
          {
            phase: "transform",
            message: "Unterminated JSX contents",
            line: 14,
            column: 8,
          },
        ]}
        source={SOURCE}
        modules={{}}
      />
    )
    expect(screen.getByText("Unterminated JSX contents")).toBeTruthy()
    expect(screen.getByText("+1 more")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { expanded: false }))
    expect(screen.getAllByText("source:14:8")).toHaveLength(2)
    const pre = document.querySelector("pre")!
    expect(pre.textContent).toBe(`line 14: const x14 = 14\n       ^`)
    expect(
      screen.getByText(
        "permissions.reads.kinds: add ada.example.com/tasks/task"
      )
    ).toBeTruthy()
    expect(screen.getByText("grant")).toBeTruthy()
    expect(screen.getByText("syntax")).toBeTruthy()
  })
})
