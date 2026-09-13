// @vitest-environment jsdom
/** The hooks over a fake app: `useRecords` renders the first page the
 * subscription answers and the next one it pushes, shares one subscription
 * between two components over one query and drops it when both leave, walks
 * the cursor page by page under `loadMore` and keeps the walk coherent
 * across a push (re-read to the same depth from the pushed cursor, the
 * cursor always the last page's, rows deduplicated by identity, every walked
 * page dropped when the grant goes), `useRecord` narrows to the id, and
 * `useRoute` follows the app's path. */

import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  SdkError,
  type App,
  type Page,
  type Query,
  type SubstrateRecord,
} from "./core"

const subscriptions = new Map<string, (page: Page) => void>()
const opened: string[] = []
const closed: string[] = []
const listed: string[] = []
let routeListeners = new Set<(path: string) => void>()
let path = ""

function row(id: string, kind = "ada.example.com/tasks/task"): SubstrateRecord {
  return {
    id,
    kind,
    properties: { name: id },
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

/** What `records.list` answers, by the cursor it was asked under. */
let pages = new Map<string, Partial<Page>>()
let deferred: { resolve(page: Page): void }[] = []

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
    list: (q: Query) => {
      listed.push(JSON.stringify(q))
      const answer = pages.get(q.after ?? "")
      if (answer === undefined) {
        return new Promise<Page>((resolve) => deferred.push({ resolve }))
      }
      if (answer.error) return Promise.reject(answer.error)
      return Promise.resolve({ records: [], loading: false, ...answer })
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

/** Let every settled list answer land. */
const settle = () =>
  act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  })

function Names({ q }: { q: Query }) {
  const page = useRecords(q)
  return (
    <div>
      <p data-testid="state">
        {page.loading
          ? "loading"
          : page.error
            ? `error:${page.error.code}`
            : "ready"}
      </p>
      <ul>
        {page.records.map((r) => (
          <li key={`${r.kind} ${r.id}`}>{r.id}</li>
        ))}
      </ul>
      {page.cursor && <button onClick={page.loadMore}>more</button>}
    </div>
  )
}

const shown = () =>
  screen.queryAllByRole("listitem").map((li) => li.textContent)
const more = () =>
  act(async () => {
    screen.getByRole("button", { name: "more" }).click()
    await Promise.resolve()
    await Promise.resolve()
  })

afterEach(() => {
  cleanup()
  subscriptions.clear()
  opened.length = 0
  closed.length = 0
  listed.length = 0
  pages = new Map()
  deferred = []
  routeListeners = new Set()
  path = ""
})

const OPEN: Query = { kind: "ada.example.com/tasks/task", orderBy: "dueAt:asc" }
const after = (cursor: string) => JSON.stringify({ ...OPEN, after: cursor })

describe("useRecords", () => {
  it("renders the answered page, then the pushed one", async () => {
    render(<Names q={OPEN} />)
    expect(screen.getByTestId("state").textContent).toBe("loading")
    expect(opened).toEqual([JSON.stringify(OPEN)])
    push(OPEN, { records: [row("a"), row("b")] })
    expect(screen.getByTestId("state").textContent).toBe("ready")
    expect(shown()).toEqual(["a", "b"])
    push(OPEN, { records: [row("b")] })
    expect(shown()).toEqual(["b"])
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
    expect(shown()).toEqual(["z"])
  })

  it("walks the cursor page by page, each under the page before it", async () => {
    pages.set("c1", { records: [row("c")], cursor: "c2" })
    pages.set("c2", { records: [row("d")] })
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await more()
    expect(listed).toEqual([after("c1")])
    expect(shown()).toEqual(["a", "c"])
    // The cursor is page two's, not page one's, so the next Load more is
    // page three and never page two again.
    await more()
    expect(listed).toEqual([after("c1"), after("c2")])
    expect(shown()).toEqual(["a", "c", "d"])
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("re-reads every walked page from the pushed cursor and keeps the walk's cursor", async () => {
    pages.set("c1", { records: [row("c")], cursor: "c2" })
    pages.set("c2", { records: [row("d")], cursor: "c3" })
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await more()
    await more()
    expect(shown()).toEqual(["a", "c", "d"])
    listed.length = 0

    // A push: the first page moved (a row landed, its cursor moved with it),
    // so the two walked pages are read again from the NEW cursor, in turn,
    // and a row edited out of page two is gone.
    pages.set("c1b", { records: [row("c")], cursor: "c2b" })
    pages.set("c2b", { records: [row("e")], cursor: "c3b" })
    push(OPEN, { records: [row("z"), row("a")], cursor: "c1b" })
    // The old pages stay up until the walk lands.
    expect(shown()).toEqual(["z", "a", "c", "d"])
    await settle()
    expect(listed).toEqual([after("c1b"), after("c2b")])
    expect(shown()).toEqual(["z", "a", "c", "e"])
    // The cursor offered is the last walked page's, not the pushed first's.
    pages.set("c3b", { records: [row("f")] })
    await more()
    expect(listed.at(-1)).toBe(after("c3b"))
    expect(shown()).toEqual(["z", "a", "c", "e", "f"])
  })

  it("deduplicates by identity across every page while a walk is stale", async () => {
    pages.set("c1", { records: [row("b"), row("c")], cursor: "c2" })
    pages.set("c2", { records: [row("c"), row("d")] })
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await more()
    await more()
    expect(shown()).toEqual(["a", "b", "c", "d"])
    // The first page now carries a row page two had; it shows once, and a
    // same id under another kind is another row.
    push(OPEN, {
      records: [row("a"), row("b"), row("d", "ada.example.com/tasks/bug")],
      cursor: "c1",
    })
    expect(shown()).toEqual(["a", "b", "d", "c", "d"])
  })

  it("drops the walked pages when the pushed page has no cursor, or the grant went", async () => {
    pages.set("c1", { records: [row("c")] })
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await more()
    expect(shown()).toEqual(["a", "c"])
    // The collection fits one page now.
    push(OPEN, { records: [row("a")] })
    expect(shown()).toEqual(["a"])
    expect(listed).toHaveLength(1)

    push(OPEN, { records: [row("a")], cursor: "c1" })
    await settle()
    await more()
    expect(shown()).toEqual(["a", "c"])
    // A forbidden push clears every retained page, not only the first.
    push(OPEN, {
      records: [],
      error: new SdkError({ code: "forbidden", message: "no longer reads" }),
    })
    expect(shown()).toEqual([])
    expect(screen.getByTestId("state").textContent).toBe("error:forbidden")
    // Another error keeps what is on screen.
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await settle()
    await more()
    push(OPEN, {
      records: [row("a")],
      cursor: "c1",
      error: new SdkError({ code: "network", message: "offline" }),
    })
    expect(shown()).toEqual(["a", "c"])
    expect(screen.getByTestId("state").textContent).toBe("error:network")
  })

  it("drops a Load more that lands after the push that outdated it", async () => {
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    // c1 is not answered yet: the walk is in flight.
    await more()
    expect(listed).toEqual([after("c1")])
    pages.set("c1b", { records: [row("c")] })
    push(OPEN, { records: [row("a"), row("b")], cursor: "c1b" })
    await settle()
    // The push re-walked to the asked depth from its own cursor …
    expect(listed).toEqual([after("c1"), after("c1b")])
    expect(shown()).toEqual(["a", "b", "c"])
    // … and the stale answer, landing now, appends nothing.
    await act(async () => {
      deferred[0].resolve({ records: [row("stale")], loading: false })
      await Promise.resolve()
    })
    expect(shown()).toEqual(["a", "b", "c"])
  })

  it("shows a failed Load more and clears it on the next push", async () => {
    pages.set("c1", { error: new SdkError({ code: "network", message: "x" }) })
    render(<Names q={OPEN} />)
    push(OPEN, { records: [row("a")], cursor: "c1" })
    await more()
    expect(screen.getByTestId("state").textContent).toBe("error:network")
    expect(shown()).toEqual(["a"])
    pages.set("c1", { records: [row("c")] })
    push(OPEN, { records: [row("a")], cursor: "c1" })
    // The asked depth is remembered: the push walks the page that failed.
    await settle()
    expect(screen.getByTestId("state").textContent).toBe("ready")
    expect(shown()).toEqual(["a", "c"])
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
