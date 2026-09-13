// @vitest-environment jsdom
/** The bar over a URL that names a value the view's own filter does not
 * admit: the chip is not pressed, the value is not offered, and the bar says
 * the pick was ignored, since the read holds to the declared filter. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import type { ViewSpec } from "@/lib/apps/spec"
import { FacetBar } from "./facet-bar"

const task: KindInfo = {
  identity: "ada.example.com/tasks/task",
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      name: { type: "string" },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
    },
  },
}

const spec: ViewSpec = {
  id: "tasks-open",
  name: "Open tasks",
  layout: "list",
  kind: task.identity,
  requiresAtLeast: {},
  filter: { properties: { status: { in: ["open", "proposed"] } } },
  orderBy: [],
  show: [],
  facets: ["status"],
  window: {},
  first: 50,
  related: [],
  attach: [],
  replaces: false,
  actions: [],
  permissions: { reads: { kinds: [] }, writes: [], call: [], agents: [] },
  problems: [],
}

function renderBar(search: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <NuqsTestingAdapter searchParams={search}>
      <QueryClientProvider client={client}>
        <FacetBar spec={spec} kind={task} kinds={[task]} records={[]} />
      </QueryClientProvider>
    </NuqsTestingAdapter>
  )
}

afterEach(cleanup)

describe("FacetBar", () => {
  it("says a URL value outside the declared filter was ignored, and presses no chip for it", () => {
    renderBar("?f.status=done")
    const narrow = screen.getByRole("group", { name: "Narrow" })
    const chips = within(narrow).getAllByRole("button")
    expect(chips.map((b) => b.textContent)).toEqual([
      "Clear",
      "open",
      "proposed",
    ])
    expect(
      chips.filter((b) => b.getAttribute("aria-pressed") === "true")
    ).toEqual([])
    expect(
      screen.getByText(
        'Status: "done" is not among the values this view shows; that chip was ignored'
      )
    ).toBeTruthy()
  })

  it("says nothing for a pick the filter admits", () => {
    renderBar("?f.status=open")
    const narrow = screen.getByRole("group", { name: "Narrow" })
    const pressed = within(narrow)
      .getAllByRole("button")
      .filter((b) => b.getAttribute("aria-pressed") === "true")
    expect(pressed.map((b) => b.textContent)).toEqual(["open"])
    expect(screen.queryByText(/that chip was ignored/)).toBeNull()
  })
})
