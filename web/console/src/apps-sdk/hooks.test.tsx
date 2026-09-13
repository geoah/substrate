// @vitest-environment jsdom
/** The hooks over a fake app: `useRecords` renders the first page the
 * subscription answers and the next one it pushes, shares one subscription
 * between two components over one query and drops it when both leave,
 * `loadMore` appends the next cursor's page, `useRecord` narrows to the id,
 * and `useRoute` follows the app's path. */

import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { App, Page, Query, SubstrateRecord } from "./core"

const subscriptions = new Map<string, (page: Page) => void>()
const opened: string[] = []
const closed: string[] = []
const listed: string[] = []
let routeListeners = new Set<(path: string) => void>()
let path = ""

function row(id: string): SubstrateRecord {
  return {
    id,
    kind: "ada.example.com/tasks/task",
    properties: { name: id },
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

const fakeApp = {
  records: {
    subscribe(q: Query, cb: (page: Page) => void) {
      const key = JSON.stringify(q)
      opened.push(key)
      subscriptions.set(key, cb)
      return () => {
        closed.push(key)
        subscriptions.delete(key)
      }
    },
    list: async (q: Query) => {
      listed.push(JSON.stringify(q))
      return { records: [row("c")], loading: false } satisfies Page
    },
  },
  route: {
    get path() {
      return path
    },
    navigate: (next: string) => {
      path = next
      for (const l of routeListeners) l(next)
    },
    onChange: (cb: (p: string) => void) => {
      routeListeners.add(cb)
      return () => routeListeners.delete(cb)
    },
  },
} as unknown as App

vi.mock("./core", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./core")>()
  return { ...actual, currentApp: () => fakeApp }
})

const { useRecord, useRecords, useRoute } = await import("./hooks")

const push = (q: Query, page: Partial<Page>) =>
  act(() => {
    subscriptions.get(JSON.stringify(q))?.({
      records: [],
      loading: false,
      ...page,
    })
  })

function Names({ q }: { q: Query }) {
  const page = useRecords(q)
  return (
    <div>
      <p data-testid="state">{page.loading ? "loading" : "ready"}</p>
      <ul>
        {page.records.map((r) => (
          <li key={r.id}>{r.id}</li>
        ))}
      </ul>
      {page.cursor && <button onClick={page.loadMore}>more</button>}
    </div>
  )
}

afterEach(() => {
  cleanup()
  subscriptions.clear()
  opened.length = 0
  closed.length = 0
  listed.length = 0
  routeListeners = new Set()
  path = ""
})

const OPEN: Query = { kind: "ada.example.com/tasks/task", orderBy: "dueAt:asc" }

describe("useRecords", () => {
  it("renders the answered page, then the pushed one", async () => {
    render(<Names q={OPEN} />)
    expect(screen.getByTestId("state").textContent).toBe("loading")
    expect(opened).toEqual([JSON.stringify(OPEN)])
    push(OPEN, { records: [row("a"), row("b")] })
    expect(screen.getByTestId("state").textContent).toBe("ready")
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["a", "b"]
    )
    push(OPEN, { records: [row("b")] })
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["b"]
    )
  })

  it("shares one subscription between two components and releases it last-out", () => {
    const { unmount } = render(
      <>
        <Names q={OPEN} />
        <Names q={OPEN} />
      </>
    )
    expect(opened).toHaveLength(1)
    push(OPEN, { records: [row("a")] })
    expect(screen.getAllByRole("listitem")).toHaveLength(2)
    unmount()
    expect(closed).toEqual([JSON.stringify(OPEN)])
  })

  it("re-subscribes when the query changes and renders the new page", () => {
    const DONE: Query = {
      kind: "ada.example.com/tasks/task",
      filter: { properties: { status: { eq: "done" } } },
    }
    const { rerender } = render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")] })
    expect(screen.getByTestId("state").textContent).toBe("ready")
    rerender(<Names q={DONE} />)
    // The old subscription is released, the new one opened, and the page
    // is loading again until it answers.
    expect(closed).toEqual([JSON.stringify(OPEN)])
    expect(opened).toEqual([JSON.stringify(OPEN), JSON.stringify(DONE)])
    expect(screen.getByTestId("state").textContent).toBe("loading")
    push(DONE, { records: [row("z")] })
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["z"]
    )
  })

  it("appends the next cursor's page on loadMore and keeps it across a push", async () => {
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await act(async () => {
      screen.getByRole("button", { name: "more" }).click()
      await Promise.resolve()
    })
    expect(listed).toEqual([JSON.stringify({ ...OPEN, after: "c1" })])
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["a", "c"]
    )
    expect(screen.queryByRole("button")).toBeNull()
    push(OPEN, { records: [row("a"), row("c")] })
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["a", "c"]
    )
  })
})

describe("useRecord", () => {
  it("subscribes to the one id and hands back the record", () => {
    function One() {
      const { record, loading } = useRecord("ada.example.com/tasks/task", "t1")
      return (
        <p>
          {loading
            ? "loading"
            : ((record?.properties.name as string) ?? "none")}
        </p>
      )
    }
    render(<One />)
    const q: Query = {
      kind: "ada.example.com/tasks/task",
      filter: { ids: ["t1"] },
      first: 1,
    }
    expect(opened).toEqual([JSON.stringify(q)])
    push(q, { records: [row("t1")] })
    expect(screen.getByText("t1")).toBeTruthy()
  })
})

describe("useRoute", () => {
  it("follows the app's path", () => {
    function Where() {
      const { path: p, navigate } = useRoute()
      return <button onClick={() => navigate("/website")}>{p || "root"}</button>
    }
    render(<Where />)
    expect(screen.getByRole("button").textContent).toBe("root")
    act(() => screen.getByRole("button").click())
    expect(screen.getByRole("button").textContent).toBe("/website")
  })
})
