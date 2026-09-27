// @vitest-environment jsdom
/** The transcript's one opening promise: a triggered thread's first user turn
 * is a delivery envelope, and it renders as the trigger's context — which
 * record's change started the run, as its mark — never as a JSON bubble.
 * Technical mode adds what fired and the raw envelope. A thread whose first
 * message is plain chat keeps its bubble. */

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

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { TurnView } from "@/lib/api/transcript"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { Transcript } from "./transcript"

const envelope = JSON.stringify({
  change: {
    actor: "agent:stories.e2e.example:e2e:matcher",
    id: "tr-chitchat",
    kind: "samples.substrate.reamde.dev/calendar/transcript",
    op: "update",
    seq: 216,
  },
  record: {
    id: "tr-chitchat",
    kind: "samples.substrate.reamde.dev/calendar/transcript",
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

function technically(children: ReactNode) {
  return (
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: true,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      {children}
    </ConsolePreferencesContext.Provider>
  )
}

const triggered = [
  turn({ content: envelope }),
  turn({ key: "t1", role: "assistant", content: "on it" }),
]

describe("the trigger context", () => {
  it("renders the first user envelope as the record whose change started it", () => {
    const { container } = render(<Transcript turns={triggered} />)
    expect(screen.getByText("Started on its own because")).toBeTruthy()
    expect(screen.getByText("changed")).toBeTruthy()
    // The delivered record, as its mark linking to it, titled off the snapshot.
    const mark = container.querySelector(
      'a[data-to="/data/$authority/$pkg/$name/$id"]'
    )
    expect(mark?.textContent).toContain("Billing migration sync")
    expect(JSON.parse(mark?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "samples.substrate.reamde.dev",
      pkg: "calendar",
      name: "transcript",
      id: "tr-chitchat",
    })
    // What fired is technical.
    expect(screen.queryByText(/changelog seq 216/)).toBeNull()
    expect(screen.queryByText("Raw envelope")).toBeNull()
  })

  it("says what fired, and keeps the raw envelope reachable, in technical mode", () => {
    render(technically(<Transcript turns={triggered} />))
    expect(screen.getByText(/changelog seq 216/)).toBeTruthy()
    expect(
      screen.getByText(/agent:stories\.e2e\.example:e2e:matcher/)
    ).toBeTruthy()
    expect(screen.getByText("Raw envelope")).toBeTruthy()
  })

  it("keeps a plain first message as the bubble it is", () => {
    render(<Transcript turns={[turn({ content: "hello there" })]} />)
    expect(screen.getByText("hello there")).toBeTruthy()
    expect(screen.queryByText("Started on its own because")).toBeNull()
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
    expect(screen.queryByText("Started on its own because")).toBeNull()
  })
})
