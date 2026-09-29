// @vitest-environment jsdom
/** The tool line: what the call did in words and whether it worked; opened,
 * what came back. A settled `propose` did not change anything — it landed a
 * row somebody has to decide — so the line carries the suggestion card with
 * its live state and its decisions, open or not. Technical mode names the
 * callable in full and shows the payloads. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    search,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    search?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a
      data-to={to}
      data-params={JSON.stringify(params ?? {})}
      data-search={JSON.stringify(search ?? {})}
      {...rest}
    >
      {children}
    </a>
  ),
}))

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import type { ToolCallView } from "@/lib/api/transcript"
import { ToolCallCard } from "./tool-call"

function call(over: Partial<ToolCallView> = {}): ToolCallView {
  return {
    id: "c1",
    name: "propose",
    arguments: '{"kind":"crew.test.dev/crew/widget","target":"w-1"}',
    output: '{"id":"cr7abc4def6k"}',
    ok: true,
    ...over,
  }
}

/** The card resolves the request it links, so the tests seed the query cache
 * with the row — the card then renders its live state without a network. */
function renderCard(
  view: ToolCallView,
  request?: SubstrateRecord,
  technical = false,
  agent?: SubstrateRecord
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  if (request) {
    client.setQueryData(
      [
        "record",
        "substrate.reamde.dev",
        "core",
        "recordpatchrequest",
        request.id,
      ],
      request
    )
  }
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <ToolCallCard call={view} agent={agent} />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

const request: SubstrateRecord = {
  id: "cr7abc4def6k",
  kind: "substrate.reamde.dev/core/recordpatchrequest",
  properties: {
    op: "patch",
    decision: "proposed",
    rationale: "tidy",
    diff: { properties: { name: "better" } },
  },
  labels: {},
  version: 1,
  createdAt: "2026-08-13T00:00:00Z",
  updatedAt: "2026-08-13T00:00:00Z",
}

afterEach(cleanup)

/** The router's Link is stubbed to an anchor carrying its route and params, so
 * the assertion is about WHERE the card points, not how the router renders. */
function reviewLink(container: HTMLElement): HTMLAnchorElement | null {
  return container.querySelector('a[data-to="/change-requests/$id"]')
}

describe("the tool line", () => {
  it("carries a settled propose's suggestion: the change in words and its decisions", () => {
    const { container } = renderCard(call(), request)
    expect(screen.getByText("Suggested a change")).toBeTruthy()
    expect(screen.getByLabelText("Done")).toBeTruthy()
    // The change itself, in words: the property's label and its new value.
    expect(screen.getByText("tidy")).toBeTruthy()
    expect(screen.getByText("Name")).toBeTruthy()
    expect(screen.getByText("better")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Apply" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeTruthy()
    // Review is the full review of this very request.
    const link = reviewLink(container)
    expect(link?.textContent).toContain("Review")
    expect(JSON.parse(link?.getAttribute("data-params") ?? "{}")).toEqual({
      id: "cr7abc4def6k",
    })
  })

  it("withholds the decisions once the suggestion is decided", () => {
    const { container } = renderCard(call(), {
      ...request,
      properties: { ...request.properties, decision: "accepted" },
    })
    expect(screen.getByText("Applied")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull()
    expect(reviewLink(container)?.textContent).toBe("See the change")
  })

  it("prefers the engine-stamped request id over the payload sniff", () => {
    const { container } = renderCard(
      call({
        name: "file",
        output: "created it",
        changes: [
          {
            seq: 4,
            op: "put",
            kind: "substrate.reamde.dev/core/recordpatchrequest",
            id: "cr7abc4def6k",
          },
        ],
      }),
      request
    )
    expect(reviewLink(container)).toBeTruthy()
  })

  it("offers no suggestion while the call is still out, or when it failed", () => {
    const running = renderCard(call({ ok: undefined, output: undefined }))
    expect(reviewLink(running.container)).toBeNull()
    cleanup()

    const failed = renderCard(
      call({ ok: false, output: '{"error":"refused"}' })
    )
    expect(reviewLink(failed.container)).toBeNull()
    expect(screen.getByText("Didn’t work")).toBeTruthy()
    // Opened, it says why in words.
    fireEvent.click(screen.getByRole("button", { name: /Suggested a change/ }))
    expect(screen.getByText("It didn’t work: refused")).toBeTruthy()
  })

  it("offers no suggestion for another tool, whatever its payload says", () => {
    const { container } = renderCard(
      call({ name: "query", arguments: '{"q":"handover"}' })
    )
    expect(reviewLink(container)).toBeNull()
    expect(screen.getByText("Searched for “handover”")).toBeTruthy()
  })

  it("says what a write changed once opened, the record as its mark", () => {
    const { container } = renderCard(
      call({
        name: "write",
        arguments:
          '{"op":"patch","kind":"crew.test.dev/crew/widget","id":"w1","input":{"properties":{"name":"w"}}}',
        output: '{"record":{"id":"w1","kind":"crew.test.dev/crew/widget"}}',
        changes: [
          {
            seq: 202,
            op: "patch",
            kind: "crew.test.dev/crew/widget",
            id: "w1",
          },
        ],
      })
    )
    expect(screen.getByText("Changed a widget")).toBeTruthy()
    const mark = () =>
      container.querySelector('a[data-to="/data/$authority/$pkg/$name/$id"]')
    // Closed, the line is the whole card, as it is for a search.
    expect(screen.queryByText("Changed")).toBeNull()
    expect(mark()).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /Changed a widget/ }))
    expect(screen.getByText("Changed")).toBeTruthy()
    expect(JSON.parse(mark()?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "crew.test.dev",
      pkg: "crew",
      name: "widget",
      id: "w1",
    })
    // The seq is technical.
    expect(screen.queryByText(/seq 202/)).toBeNull()
  })

  it("says what a live write changed before the row is stamped", () => {
    const { container } = renderCard(
      call({
        name: "write",
        arguments: '{"op":"create","kind":"crew.test.dev/crew/widget"}',
        output: '{"record":{"id":"w2","kind":"crew.test.dev/crew/widget"}}',
      })
    )
    fireEvent.click(screen.getByRole("button", { name: /Saved a widget/ }))
    expect(screen.getByText("Saved")).toBeTruthy()
    const mark = container.querySelector(
      'a[data-to="/data/$authority/$pkg/$name/$id"]'
    )
    expect(JSON.parse(mark?.getAttribute("data-params") ?? "{}")).toMatchObject(
      { id: "w2" }
    )
  })

  it("lists what a search found once opened, and says when nothing matched", () => {
    const found = renderCard(
      call({
        name: "query",
        arguments: '{"q":"cups"}',
        output:
          '{"records":[{"kind":"crew.test.dev/crew/widget","id":"w1"},{"kind":"crew.test.dev/crew/widget","id":"w2"}]}',
      })
    )
    expect(screen.queryByText("Found")).toBeNull()
    fireEvent.click(
      screen.getByRole("button", { name: /Searched for “cups”, found 2/ })
    )
    expect(screen.getByText("Found")).toBeTruthy()
    expect(
      found.container.querySelectorAll(
        'a[data-to="/data/$authority/$pkg/$name/$id"]'
      )
    ).toHaveLength(2)
    cleanup()

    renderCard(
      call({
        name: "query",
        arguments: '{"q":"cups"}',
        output: '{"records":[]}',
      })
    )
    fireEvent.click(screen.getByRole("button", { name: /found 0/ }))
    expect(screen.getByText("Nothing matched.")).toBeTruthy()
  })

  it("does not open a call whose check mark already said everything", () => {
    renderCard(
      call({
        name: "write",
        arguments: '{"op":"patch","kind":"crew.test.dev/crew/widget"}',
        output: '{"record":null}',
      })
    )
    expect(screen.getByText("Changed a widget")).toBeTruthy()
    expect(screen.getByLabelText("Done")).toBeTruthy()
    expect(screen.queryByRole("button")).toBeNull()
    expect(screen.queryByText(/It worked/)).toBeNull()
  })

  it("shows a sub-agent's reply and a function's output once opened", () => {
    const agent: SubstrateRecord = {
      id: "crew.test.dev/crew/lead",
      kind: "substrate.reamde.dev/core/agent",
      properties: {
        subagents: [
          { ref: "substrate.reamde.dev/core/agent/crew.test.dev/crew/scout" },
        ],
        tools: [
          {
            function: {
              ref: "substrate.reamde.dev/core/function/crew.test.dev/crew/fetch",
            },
          },
        ],
      },
      labels: {},
      version: 1,
      createdAt: "2026-08-13T00:00:00Z",
      updatedAt: "2026-08-13T00:00:00Z",
    }
    const { container } = renderCard(
      call({
        name: "scout",
        arguments: '{"input":"look around"}',
        output:
          '{"reply":"Two widgets are overdue.","thread":"t1","status":"ok"}',
      }),
      undefined,
      false,
      agent
    )
    fireEvent.click(screen.getByRole("button", { name: /Asked Scout/ }))
    expect(screen.getByText("Scout replied")).toBeTruthy()
    expect(screen.getByText("Two widgets are overdue.")).toBeTruthy()
    const link = container.querySelector('a[data-to="/agents"]')
    expect(link?.textContent).toBe("Open its conversation")
    cleanup()

    renderCard(
      call({
        name: "fetch",
        arguments: '{"url":"https://example.com"}',
        output: '{"output":"<title>Example</title>","effects":0}',
      }),
      undefined,
      false,
      agent
    )
    fireEvent.click(screen.getByRole("button", { name: /Used fetch/ }))
    expect(screen.getByText("What it returned")).toBeTruthy()
    expect(screen.getByText("<title>Example</title>")).toBeTruthy()
  })

  describe("a sub-agent chain's writes", () => {
    const scoutCall = (over: Partial<ToolCallView> = {}) =>
      call({
        name: "scout",
        callable: "agent:crew.test.dev:crew:scout",
        arguments: '{"input":"file it"}',
        output: '{"reply":"Filed.","thread":"th1","status":"ok"}',
        subagentWrites: {
          thread: "th1",
          records: 3,
          kinds: ["crew.test.dev/crew/task", "crew.test.dev/crew/note"],
          moreKinds: 0,
        },
        ...over,
      })
    const links = (container: HTMLElement) =>
      Array.from(container.querySelectorAll('a[data-to="/agents"]'))

    it("says what the chain wrote in a sentence, linking the child thread once", () => {
      const { container } = renderCard(scoutCall())
      fireEvent.click(screen.getByRole("button", { name: /Asked Scout/ }))
      expect(screen.getByText("Wrote 3 records across task, note")).toBeTruthy()
      // The reply still reads, and the one link into the child thread rides
      // the writes line.
      expect(screen.getByText("Filed.")).toBeTruthy()
      const found = links(container)
      expect(found).toHaveLength(1)
      expect(found[0].textContent).toBe("Open its conversation")
      expect(JSON.parse(found[0].getAttribute("data-search") ?? "{}")).toEqual({
        thread: "th1",
      })
    })

    it("names each kind by its full reference in technical mode", () => {
      const { container } = renderCard(
        scoutCall({
          subagentWrites: {
            thread: "th1",
            records: 30,
            kinds: ["crew.test.dev/crew/task", "crew.test.dev/crew/note"],
            moreKinds: 5,
          },
        }),
        undefined,
        true
      )
      fireEvent.click(screen.getByRole("button", { name: /Asked Scout/ }))
      const line = links(container)[0].parentElement
      expect(line?.textContent).toContain(
        "Wrote 30 records across crew.test.dev/crew/task, crew.test.dev/crew/note and 5 more kinds"
      )
    })

    it("says a chain that wrote nothing wrote nothing", () => {
      renderCard(
        scoutCall({
          subagentWrites: {
            thread: "th1",
            records: 0,
            kinds: [],
            moreKinds: 0,
          },
        })
      )
      fireEvent.click(screen.getByRole("button", { name: /Asked Scout/ }))
      expect(screen.getByText("Wrote no records")).toBeTruthy()
    })

    it("still says what a failed chain wrote, and where", () => {
      const { container } = renderCard(
        scoutCall({
          ok: false,
          output: '{"error":"llm: scripted failure 500"}',
          subagentWrites: {
            thread: "th1",
            records: 1,
            kinds: ["crew.test.dev/crew/memo"],
            moreKinds: 0,
          },
        })
      )
      fireEvent.click(screen.getByRole("button", { name: /Asked Scout/ }))
      expect(screen.getByText(/It didn’t work/)).toBeTruthy()
      expect(screen.getByText("Wrote 1 record of memo")).toBeTruthy()
      expect(links(container)).toHaveLength(1)
    })
  })

  it("names the callable and shows the payloads in technical mode", () => {
    renderCard(
      call({
        name: "write",
        arguments: '{"op":"patch"}',
        output: '{"ok":1}',
        changes: [
          {
            seq: 202,
            op: "patch",
            kind: "crew.test.dev/crew/widget",
            id: "w1",
          },
        ],
      }),
      undefined,
      true
    )
    // No stamp: the name resolves, spelled as the rows spell a callable.
    expect(
      screen.getByText("function:substrate.reamde.dev:core:write")
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /Made a change/ }))
    expect(screen.getByText("patch · seq 202")).toBeTruthy()
    expect(screen.getByText("Request")).toBeTruthy()
    expect(screen.getByText("Response")).toBeTruthy()
  })

  it("reads a stamped callable over an alias the agent no longer carries", () => {
    const agent: SubstrateRecord = {
      id: "crew.test.dev/crew/lead",
      kind: "substrate.reamde.dev/core/agent",
      properties: {
        tools: [
          {
            function: {
              ref: "substrate.reamde.dev/core/function/crew.test.dev/crew/fetch",
            },
          },
        ],
      },
      labels: {},
      version: 1,
      createdAt: "2026-08-13T00:00:00Z",
      updatedAt: "2026-08-13T00:00:00Z",
    }
    const stamped = "function:substrate.reamde.dev:core:query"
    renderCard(
      call({
        name: "lookup",
        callable: stamped,
        arguments: '{"q":"cups"}',
        output: '{"records":[]}',
      }),
      undefined,
      true,
      agent
    )
    // The words come from what ran, and the callable is shown whole.
    expect(screen.getByText("Searched for “cups”, found 0")).toBeTruthy()
    expect(screen.getByText(stamped)).toBeTruthy()
    expect(screen.queryByText("lookup")).toBeNull()
  })
})
