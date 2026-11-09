import { describe, expect, it } from "vitest"
import { kindStatus, moveTo, movesFrom, renames, standing } from "./status"

describe("kindStatus", () => {
  it("reads a machine's standings and moves, as the Swift kit does", () => {
    const status = kindStatus({
      traits: [
        "substrate.reamde.dev/core/temporal(point: dueAt)",
        "substrate.reamde.dev/core/lifecycle",
      ],
      properties: {
        status: {
          type: "state",
          states: ["proposed", "open", "done", "abandoned"],
          initial: "open",
          transitions: [
            { from: "proposed", to: "open" },
            { from: "proposed", to: "abandoned" },
            { from: "open", to: "done" },
            { from: "open", to: "abandoned" },
            { from: "done", to: "open" },
          ],
          done: ["done"],
          dropped: ["abandoned"],
          proposed: ["proposed"],
        },
      },
    })!
    expect(status.property).toBe("status")
    expect(standing(status, "proposed")).toBe("proposed")
    expect(standing(status, "abandoned")).toBe("dropped")
    expect(standing(status, undefined)).toBe("open")
    expect(moveTo(status, "proposed", "open")).toBe("open")
    expect(moveTo(status, "proposed", "dropped")).toBe("abandoned")
    expect(moveTo(status, "proposed", "done")).toBeUndefined()
    expect(movesFrom(status, "done")).toEqual(["open"])
  })

  it("reads a renamed enum, which takes any of its values", () => {
    const status = kindStatus({
      traits: ["substrate.reamde.dev/core/lifecycle(status: state)"],
      properties: {
        state: {
          type: "enum",
          values: ["todo", { value: "closed", label: "Closed" }],
          done: ["closed"],
        },
      },
    })!
    expect(status.property).toBe("state")
    expect(status.values).toEqual(["todo", "closed"])
    expect(moveTo(status, "todo", "done")).toBe("closed")
  })

  it("answers nothing for a kind without the trait", () => {
    expect(
      kindStatus({ properties: { status: { type: "enum", values: ["a"] } } })
    ).toBeUndefined()
    expect(renames("substrate.reamde.dev/core/temporal(point: dueAt)")).toEqual(
      { point: "dueAt" }
    )
  })
})
