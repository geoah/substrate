// @vitest-environment jsdom
/** The conversation's shape: your messages as bubbles, the agent's
 * consecutive turns as ONE reply under one mark, its prose rendered with the
 * records it names as their marks, and the substrate's own decisions as a
 * quiet line. */

import { cleanup, render, screen } from "@testing-library/react"
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

import type { TurnView } from "@/lib/api/transcript"
import { Transcript } from "./transcript"

const turn = (over: Partial<TurnView>): TurnView => ({
  key: "t0",
  role: "user",
  content: "",
  tools: [],
  ...over,
})

afterEach(cleanup)

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
})
