/** The transcript's one opening promise: a triggered thread's first user turn
 * is a delivery envelope, and it renders as the trigger's context — what fired
 * and which record arrived, as a pill — never as a JSON bubble. A thread whose
 * first message is plain chat keeps its bubble. */

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

const envelope = JSON.stringify({
  change: {
    actor: "agent:stories.e2e.example:matcher",
    id: "tr-chitchat",
    kind: "calendar.substrate.reamde.dev/transcript",
    op: "update",
    seq: 216,
  },
  record: {
    id: "tr-chitchat",
    kind: "calendar.substrate.reamde.dev/transcript",
    properties: { title: "Billing migration sync" },
  },
  repository: { owner: "e2e" },
})

const turn = (over: Partial<TurnView>): TurnView => ({
  key: "t0",
  role: "user",
  content: "",
  tools: [],
  ...over,
})

afterEach(cleanup)

describe("the trigger context", () => {
  it("renders the first user envelope as what fired + the delivered record", () => {
    const { container } = render(
      <Transcript
        turns={[
          turn({ content: envelope }),
          turn({ key: "t1", role: "assistant", content: "on it" }),
        ]}
      />
    )
    // What fired: the op, the change's address, its seq and actor.
    expect(screen.getByText("update")).toBeTruthy()
    expect(
      screen.getByText(
        /calendar\.substrate\.reamde\.dev\/transcript\/tr-chitchat/
      )
    ).toBeTruthy()
    expect(screen.getByText(/changelog seq 216/)).toBeTruthy()
    expect(screen.getByText(/agent:stories\.e2e\.example:matcher/)).toBeTruthy()
    // The delivered record, as a pill linking to it, titled off the snapshot.
    const pill = container.querySelector(
      'a[data-to="/data/$authority/$name/$id"]'
    )
    expect(pill?.textContent).toContain("Billing migration sync")
    expect(JSON.parse(pill?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "calendar.substrate.reamde.dev",
      name: "transcript",
      id: "tr-chitchat",
    })
    // The raw envelope stays reachable, collapsed.
    expect(screen.getByText("raw envelope")).toBeTruthy()
  })

  it("keeps a plain first message as the bubble it is", () => {
    render(<Transcript turns={[turn({ content: "hello there" })]} />)
    expect(screen.getByText("hello there")).toBeTruthy()
    expect(screen.queryByText("raw envelope")).toBeNull()
  })

  it("reads only the FIRST turn as a delivery", () => {
    render(
      <Transcript
        turns={[
          turn({ content: "hi" }),
          turn({ key: "t1", content: envelope }),
        ]}
      />
    )
    // The later JSON-shaped user message is a message somebody sent.
    expect(screen.queryByText("raw envelope")).toBeNull()
  })
})
