// @vitest-environment jsdom
/** The conversation's shape: your messages as bubbles, the agent's
 * consecutive turns as ONE reply under one mark, its prose rendered with the
 * records it names as their marks, and the substrate's own decisions as a
 * quiet line. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

const prefs = vi.hoisted(() => ({ technical: false }))
vi.mock("@/hooks/use-console-preferences", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/use-console-preferences")>()),
  useTechnicalDetails: () => [prefs.technical, () => {}],
}))

import type { TurnView } from "@/lib/api/transcript"
import { Transcript } from "./transcript"

const turn = (over: Partial<TurnView>): TurnView => ({
  key: "t0",
  role: "user",
  content: "",
  tools: [],
  ...over,
})

afterEach(() => {
  cleanup()
  prefs.technical = false
})

describe("the transcript", () => {
  it("folds consecutive agent turns into one reply under one mark", () => {
    const { container } = render(
      <Transcript
        agentId="ada.localhost/llm/substrate"
        turns={[
          turn({ content: "What is left?" }),
          turn({
            key: "a1",
            role: "assistant",
            tools: [
              { id: "c1", name: "query", arguments: '{"q":"left"}', ok: true },
            ],
          }),
          turn({ key: "a2", role: "assistant", content: "Two things." }),
        ]}
      />
    )
    expect(
      container.querySelectorAll('[data-slot="user-message"]')
    ).toHaveLength(1)
    expect(
      container.querySelectorAll('[data-slot="agent-message"]')
    ).toHaveLength(1)
    expect(container.querySelectorAll('[data-actor="agent"]')).toHaveLength(1)
    expect(screen.getByText("Searched for “left”")).toBeTruthy()
    expect(screen.getByText("Two things.")).toBeTruthy()
  })

  it("renders the records a reply names as their marks", () => {
    const { container } = render(
      <Transcript
        turns={[
          turn({
            key: "a1",
            role: "assistant",
            content: "- ada.localhost/tasks/task/x052 is **late**",
          }),
        ]}
      />
    )
    const mark = container.querySelector(
      'a[data-to="/data/$authority/$pkg/$name/$id"]'
    )
    expect(JSON.parse(mark?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "ada.localhost",
      pkg: "tasks",
      name: "task",
      id: "x052",
    })
    expect(container.querySelector("li strong")?.textContent).toBe("late")
  })

  it("says a decision in words", () => {
    render(
      <Transcript
        turns={[
          turn({
            key: "s1",
            role: "system",
            content: JSON.stringify({
              event: "proposalDecision",
              request: "substrate.reamde.dev/core/recordpatchrequest/cr1",
              decision: "rejected",
              op: "patch",
              target: "ada.localhost/tasks/task/x052",
            }),
          }),
        ]}
      />
    )
    expect(screen.getByText("You dismissed the change to")).toBeTruthy()
  })

  it("shows a summary as one folded line that opens to the summary text", () => {
    const { container } = render(
      <Transcript
        agentId="ada.localhost/llm/substrate"
        turns={[
          turn({ content: "What is left?" }),
          turn({ key: "a1", role: "assistant", content: "Two things." }),
          turn({
            key: "s1",
            role: "summary",
            content: "## Goal\nShip the **report**.",
            compaction: { from: "m1", through: "m9", tokensBefore: 180000 },
          }),
          turn({ key: "a2", role: "assistant", content: "Next one." }),
        ]}
      />
    )
    const toggle = screen.getByRole("button", {
      name: "Earlier conversation compacted",
    })
    expect(screen.queryByText("report")).toBeNull()
    // The summary splits the agent's turns: it is not something it said.
    expect(
      container.querySelectorAll('[data-slot="agent-message"]')
    ).toHaveLength(2)
    // Everyday mode says nothing about ranges or tokens.
    expect(screen.queryByText(/tokens/)).toBeNull()
    fireEvent.click(toggle)
    expect(container.querySelector("strong")?.textContent).toBe("report")
  })

  it("shows a live compaction as the same line, with nothing to open", () => {
    render(
      <Transcript
        turns={[
          turn({
            key: "live-c1",
            role: "summary",
            compaction: { tokensBefore: 900, covered: 6 },
          }),
        ]}
      />
    )
    expect(screen.getByText("Earlier conversation compacted")).toBeTruthy()
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("says what a compaction covered and cost in technical mode", () => {
    prefs.technical = true
    render(
      <Transcript
        turns={[
          turn({
            key: "s1",
            role: "summary",
            content: "Summary.",
            compaction: {
              from: "m1",
              through: "m9",
              tokensBefore: 180000,
              model: "claude-opus-5",
              promptTokens: 2000,
              completionTokens: 400,
            },
          }),
        ]}
      />
    )
    expect(
      screen.getByText(
        `The conversation had reached ${(180000).toLocaleString()} tokens. claude-opus-5 wrote the summary with ${(2400).toLocaleString()} tokens.`
      )
    ).toBeTruthy()
    expect(screen.getByText("m1")).toBeTruthy()
    expect(screen.getByText("m9")).toBeTruthy()
  })
})
