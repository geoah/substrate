// @vitest-environment jsdom
/** The thinking disclosure: how long the model thought, the text folded
 * behind it, and nothing at all for a turn without reasoning. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import type { TurnView } from "@/lib/api/transcript"
import { thinkingDuration, thinkingLabel } from "@/lib/thinking-time"
import { Reasoning } from "./reasoning"

const turn = (over: Partial<TurnView>): TurnView => ({
  key: "a0",
  role: "assistant",
  content: "",
  tools: [],
  ...over,
})

afterEach(cleanup)

describe("the thinking disclosure", () => {
  it("says how long a stored turn thought and folds the text", () => {
    render(
      <Reasoning
        turn={turn({ reasoning: "weigh the options", reasoningMillis: 12000 })}
      />
    )
    const trigger = screen.getByText("Thought for 12 s")
    expect(screen.queryByText("weigh the options")).toBeNull()
    fireEvent.click(trigger)
    expect(screen.getByText("weigh the options")).toBeTruthy()
  })

  it("counts while a live turn is still thinking", () => {
    render(
      <Reasoning
        turn={turn({
          reasoning: "hmm",
          reasoningStartedAt: Date.now() - 3000,
          thinking: true,
        })}
      />
    )
    expect(screen.getByText("Thinking for 3 s")).toBeTruthy()
  })

  it("renders nothing for a turn without reasoning", () => {
    const { container } = render(<Reasoning turn={turn({ content: "hi" })} />)
    expect(container.innerHTML).toBe("")
  })

  it("words durations the way a reader says them", () => {
    expect(thinkingDuration(200)).toBe("1 s")
    expect(thinkingDuration(59_400)).toBe("59 s")
    expect(thinkingDuration(65_000)).toBe("1 min 5 s")
    expect(thinkingDuration(120_000)).toBe("2 min")
    expect(thinkingLabel({}, 0)).toBe("Thought")
    // A live turn that moved on to its text reads its measured span.
    expect(
      thinkingLabel(
        { reasoningStartedAt: 0, reasoningMillis: 2000, thinking: false },
        99_000
      )
    ).toBe("Thought for 2 s")
  })
})
