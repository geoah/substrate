// @vitest-environment jsdom
/** An applet in the shell against a mocked server: the first frame names the
 * start screen in the address; a tap posts the action with the state the
 * renderer holds and a new screen is a step Back walks; a question waits
 * for a yes, answered in its own words, and is sent again confirmed; a
 * notice shows once; an edit under a refreshOn path renders again once the
 * typing stops; a missing applet, a screen that cannot open and a refused
 * action read as themselves; and full screen, a screen named like its applet
 * is named once. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import { saveSession } from "@/lib/api/session"
import type { A2UIMessage, AppletFrame } from "@/lib/api/types"
import {
  appletLocation,
  appletLocationSearch,
  appletSearch,
} from "@/lib/applet-location"

import { ShellAppletView, StandaloneAppletView } from "./applet-view"

const BASE = "/api/v1/substrate.reamde.dev/applets/applet/books"
const CATALOG =
  "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"

const BOOKS = [
  { id: "b1", properties: { title: "Dune" } },
  { id: "b2", properties: { title: "Emma" } },
]

const LIBRARY = [
  { id: "root", component: "Column", children: ["search", "list", "clear"] },
  {
    id: "search",
    component: "TextField",
    label: "Search",
    value: { "@path": "/ui/q" },
  },
  {
    id: "list",
    component: "List",
    children: { path: "/data/books", componentId: "row" },
  },
  {
    id: "row",
    component: "Button",
    variant: "borderless",
    child: "rowText",
    action: { event: { name: "open", context: { id: { "@path": "id" } } } },
  },
  {
    id: "rowText",
    component: "Text",
    text: { "@call": "formatString", args: { value: "${properties/title}" } },
  },
  {
    id: "clear",
    component: "Button",
    child: "clearText",
    action: { event: { name: "clear" } },
  },
  { id: "clearText", component: "Text", text: "Clear the shelf" },
]

const BOOK = [
  { id: "root", component: "Column", children: ["title"] },
  {
    id: "title",
    component: "Text",
    text: { "@path": "/data/book/properties/title" },
  },
]

function created(
  components: unknown[],
  dataModel: Record<string, unknown>
): A2UIMessage[] {
  return [
    { version: "1.0", deleteSurface: { surfaceId: "applet" } },
    {
      version: "1.0",
      createSurface: {
        surfaceId: "applet",
        catalogId: CATALOG,
        components: components as Record<string, unknown>[],
        dataModel,
      },
    },
  ]
}

function library(books = BOOKS, over: Partial<AppletFrame> = {}): AppletFrame {
  return {
    applet: {
      id: "books",
      name: "Books",
      icon: "book-open",
      version: 3,
      title: "Library",
      start: "library",
      refreshOn: ["/ui/q"],
    },
    state: { screen: "library", ui: { q: "" } },
    shown: { screen: "library", version: 3 },
    messages: created(LIBRARY, { ui: { q: "" }, data: { books } }),
    ...over,
  }
}

function book(): AppletFrame {
  return {
    applet: {
      id: "books",
      name: "Books",
      version: 3,
      title: "Dune",
      start: "library",
    },
    state: { screen: "book", params: { id: "b1" }, ui: { q: "" } },
    shown: { screen: "book", version: 3 },
    messages: created(BOOK, {
      ui: { q: "" },
      params: { id: "b1" },
      data: { book: BOOKS[0] },
    }),
  }
}

type Reply = (body: Record<string, unknown>) => Response | AppletFrame
let onRender: Reply
let onAct: Reply

const calls = () =>
  vi
    .mocked(fetch)
    .mock.calls.filter(([, init]) => init?.method === "POST")
    .map(([url, init]) => ({
      verb: String(url).slice(BASE.length + 1),
      body: JSON.parse(String(init?.body)) as Record<string, unknown>,
    }))

function serve() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const url = String(input)
      const body = JSON.parse(String(init?.body ?? "{}")) as Record<
        string,
        unknown
      >
      const reply =
        url === `${BASE}/render`
          ? onRender(body)
          : url === `${BASE}/act`
            ? onAct(body)
            : Response.json({})
      return reply instanceof Response ? reply : Response.json(reply)
    })
  )
}

function Harness({ standalone = false }: { standalone?: boolean }) {
  const { id = "" } = useParams({ strict: false })
  const search = useSearch({ strict: false })
  const navigate = useNavigate()
  const View = standalone ? StandaloneAppletView : ShellAppletView
  return (
    <View
      key={id}
      appletId={id}
      location={appletLocation(search)}
      navigate={(to, { replace }) =>
        void navigate({
          to: standalone ? "/a/$id" : "/applets/$id",
          params: { id },
          search: appletLocationSearch(to),
          replace,
        } as never)
      }
    />
  )
}

function mount(initial = "/applets/books") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <Toaster>
          <Outlet />
        </Toaster>
      </QueryClientProvider>
    ),
  })
  const appletRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/applets/$id",
    validateSearch: appletSearch,
    component: () => <Harness />,
  })
  const standaloneRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/a/$id",
    validateSearch: appletSearch,
    component: () => <Harness standalone />,
  })
  const stubs = ["/applets", "/data/$authority/$pkg/$name/$id"].map((path) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path,
      component: () => null,
    })
  )
  const router = createRouter({
    routeTree: rootRoute.addChildren([appletRoute, standaloneRoute, ...stubs]),
    history: createMemoryHistory({ initialEntries: [initial] }),
  })
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
  return router
}

beforeEach(() => {
  saveSession("secret", "ada.example.com", "token-1")
  onRender = () => library()
  onAct = () => library()
  serve()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("an applet in the shell", () => {
  it("opens on its start screen and names it in the address", async () => {
    const router = mount()
    expect(await screen.findByRole("heading", { name: "Library" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Dune" })).toBeTruthy()
    expect(calls()).toEqual([{ verb: "render", body: {} }])
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ screen: "library" })
    )
    // Naming the screen replaced the entry: there is nothing to go Back to.
    expect(screen.queryByRole("button", { name: "Back" })).toBeNull()
    expect(
      screen
        .getByRole("link", { name: /Open full screen/ })
        .getAttribute("href")
    ).toBe("/a/books?screen=library")
    expect(
      screen.getByRole("link", { name: /Definition/ }).getAttribute("href")
    ).toBe("/data/substrate.reamde.dev/applets/applet/books")
  })

  it("opens the screen an address names, without its /ui", async () => {
    onRender = () => book()
    mount("/applets/books?screen=book&p.id=b1")
    expect(await screen.findByRole("heading", { name: "Dune" })).toBeTruthy()
    expect(calls()[0].body).toEqual({
      state: { screen: "book", params: { id: "b1" } },
    })
  })

  it("posts a tap with the state it holds, and Back walks to the screen before", async () => {
    const router = mount()
    await screen.findByRole("heading", { name: "Library" })
    onAct = () => book()
    fireEvent.click(screen.getByRole("button", { name: "Dune" }))
    expect(await screen.findByRole("heading", { name: "Dune" })).toBeTruthy()
    expect(calls()[1]).toEqual({
      verb: "act",
      body: {
        state: { screen: "library", ui: { q: "" } },
        shown: { screen: "library", version: 3 },
        action: {
          name: "open",
          context: { id: "b1" },
          sourceComponentId: "row",
        },
      },
    })
    expect(router.state.location.search).toEqual({
      screen: "book",
      "p.id": "b1",
    })

    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    expect(await screen.findByRole("heading", { name: "Library" })).toBeTruthy()
    expect(calls()[2]).toEqual({
      verb: "render",
      body: {
        state: { screen: "library", ui: { q: "" } },
        shown: { screen: "book", version: 3 },
      },
    })
  })

  it("asks an action's question, and sends it again confirmed", async () => {
    mount()
    await screen.findByRole("heading", { name: "Library" })
    onAct = (body) =>
      body.confirmed
        ? library([], { notice: "The shelf is clear" })
        : library(BOOKS, { confirm: "Remove every book?", messages: [] })
    fireEvent.click(screen.getByRole("button", { name: "Clear the shelf" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Remove every book?")
    // Nothing ran: the shelf is as it was, behind the dialog.
    expect(
      screen.getByRole("button", { name: "Dune", hidden: true })
    ).toBeTruthy()
    // No answer named: the generic one, and no line about nothing having
    // happened.
    expect(dialog.textContent).not.toContain("Nothing has changed")
    fireEvent.click(within(dialog).getByRole("button", { name: "Continue" }))
    expect(await screen.findByText("The shelf is clear")).toBeTruthy()
    const acts = calls().filter((c) => c.verb === "act")
    expect(acts).toHaveLength(2)
    expect(acts[0].body.confirmed).toBeUndefined()
    expect(acts[1].body).toMatchObject({
      action: { name: "clear" },
      confirmed: true,
    })
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Dune" })).toBeNull()
    )
  })

  it("renders again once the typing under a refreshOn path stops", async () => {
    mount()
    await screen.findByRole("heading", { name: "Library" })
    onRender = (body) => {
      const q = (
        (body.state as { ui?: { q?: string } }).ui?.q ?? ""
      ).toLowerCase()
      return {
        ...library(),
        // The same screen of the same version: data alone, so the input
        // keeps its text.
        messages: [
          {
            version: "1.0",
            updateDataModel: {
              surfaceId: "applet",
              path: "/data/books",
              value: BOOKS.filter((b) =>
                b.properties.title.toLowerCase().includes(q)
              ),
            },
          },
        ],
      }
    }
    const input = screen.getByLabelText<HTMLInputElement>("Search")
    for (const text of ["d", "du", "dun"])
      fireEvent.change(input, { target: { value: text } })
    expect(calls().filter((c) => c.verb === "render")).toHaveLength(1)
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Emma" })).toBeNull()
    )
    const renders = calls().filter((c) => c.verb === "render")
    expect(renders).toHaveLength(2)
    expect(renders[1].body).toEqual({
      state: { screen: "library", ui: { q: "dun" } },
      shown: { screen: "library", version: 3 },
    })
    expect(input.value).toBe("dun")
    expect(screen.getByRole("button", { name: "Dune" })).toBeTruthy()
  })

  it("says an applet that is not there", async () => {
    onRender = () =>
      Response.json(
        { error: { code: "not_found", message: "no such applet" } },
        { status: 404 }
      )
    mount()
    expect(
      await screen.findByRole("heading", { name: "No applet named “books”" })
    ).toBeTruthy()
  })

  it("keeps the screen when an action is refused, and says why", async () => {
    mount()
    await screen.findByRole("heading", { name: "Library" })
    onAct = () =>
      Response.json(
        {
          error: {
            code: "validation",
            message: "the write was refused",
            problemDetails: [
              { path: "properties.title", message: "is required" },
            ],
          },
        },
        { status: 422 }
      )
    fireEvent.click(screen.getByRole("button", { name: "Clear the shelf" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("properties.title is required")
    // The problems say it; the error chain's own words do not.
    expect(alert.textContent).not.toContain("the write was refused")
    // Pinned under the screen, where the owner who tapped is looking.
    expect(alert.getAttribute("data-slot")).toBe("applet-refusal")
    expect(alert.className).toContain("sticky")
    const surface = document.querySelector("[data-a2ui-surface]")!
    expect(
      surface.compareDocumentPosition(alert) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(screen.getByRole("button", { name: "Dune" })).toBeTruthy()
    fireEvent.click(within(alert).getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("asks in the action's own words, and draws a deleting answer as one", async () => {
    mount()
    await screen.findByRole("heading", { name: "Library" })
    onAct = (body) =>
      body.confirmed
        ? library([])
        : library(BOOKS, {
            confirm: "Remove every book?",
            confirmLabel: "Remove",
            confirmDanger: true,
            messages: [],
          })
    fireEvent.click(screen.getByRole("button", { name: "Clear the shelf" }))
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).getByRole("heading").textContent).toBe(
      "Remove every book?"
    )
    expect(
      within(dialog).queryByRole("button", { name: "Continue" })
    ).toBeNull()
    const remove = within(dialog).getByRole("button", { name: "Remove" })
    expect(remove.className).toContain("text-destructive")
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeTruthy()
    fireEvent.click(remove)
    await waitFor(() =>
      expect(calls().filter((c) => c.verb === "act")).toHaveLength(2)
    )
  })

  it("draws a button whose action deletes as destructive", async () => {
    onRender = () =>
      library(BOOKS, {
        applet: { ...library().applet, destructive: ["clear"] },
      })
    mount()
    await screen.findByRole("heading", { name: "Library" })
    expect(
      screen.getByRole("button", { name: "Clear the shelf" }).className
    ).toContain("text-destructive")
    expect(
      screen.getByRole("button", { name: "Dune" }).className
    ).not.toContain("text-destructive")
  })

  it("marks the tapped button busy while its action runs", async () => {
    mount()
    await screen.findByRole("heading", { name: "Library" })
    let reply: (frame: AppletFrame) => void = () => {}
    vi.mocked(fetch).mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          reply = (frame) => resolve(Response.json(frame))
        })
    )
    fireEvent.click(screen.getByRole("button", { name: "Clear the shelf" }))
    const clear = screen.getByRole<HTMLButtonElement>("button", {
      name: "Clear the shelf",
    })
    expect(clear.getAttribute("aria-busy")).toBe("true")
    const dune = screen.getByRole<HTMLButtonElement>("button", { name: "Dune" })
    expect(dune.disabled).toBe(true)
    expect(dune.getAttribute("aria-busy")).toBeNull()
    reply(library())
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Clear the shelf" })
          .getAttribute("aria-busy")
      ).toBeNull()
    )
  })

  it("keeps its header when a screen can't open, and offers the start", async () => {
    onRender = (body) =>
      body.state
        ? Response.json(
            {
              error: {
                code: "validation",
                message:
                  "substrate: validation failed: [screens.book.sources.book: no record b9]",
                problemDetails: [
                  {
                    path: "screens.book.sources.book",
                    message: "no record b9",
                  },
                ],
              },
            },
            { status: 422 }
          )
        : library()
    const router = mount("/applets/books?screen=book&p.id=b9")
    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "This screen couldn’t open",
      })
    ).toBeTruthy()
    // The page still names the applet: its id until anything says more.
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("books")
    expect(screen.getByRole("link", { name: /Open full screen/ })).toBeTruthy()
    expect(document.body.textContent).toContain("no record b9")
    expect(document.body.textContent).not.toContain("validation failed")
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: /Back to the start/ }))
    expect(await screen.findByRole("heading", { name: "Library" })).toBeTruthy()
    expect(
      calls()
        .filter((c) => c.verb === "render")
        .at(-1)?.body
    ).toEqual({})
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ screen: "library" })
    )
  })

  it("goes Back to the start from a screen opened with nothing behind it", async () => {
    onRender = (body) =>
      (body.state as { screen?: string } | undefined)?.screen === "book"
        ? book()
        : library()
    mount("/applets/books?screen=book&p.id=b1")
    expect(await screen.findByRole("heading", { name: "Dune" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    expect(await screen.findByRole("heading", { name: "Library" })).toBeTruthy()
    expect(calls().at(-1)).toMatchObject({
      verb: "render",
      body: { state: { screen: "library" } },
    })
    // The start, reached so, has nothing behind it.
    expect(screen.queryByRole("button", { name: "Back" })).toBeNull()
  })
})

describe("an applet full screen", () => {
  it("names a screen titled like its applet once, and has no Back on the start", async () => {
    onRender = () =>
      library(BOOKS, { applet: { ...library().applet, title: "Books" } })
    mount("/a/books")
    expect(
      await screen.findByRole("heading", { level: 1, name: "Books" })
    ).toBeTruthy()
    await waitFor(() => expect(document.title).toBe("Books"))
    expect(screen.queryByRole("button", { name: "Back" })).toBeNull()
  })

  it("names the screen and its applet when they differ", async () => {
    mount("/a/books")
    await screen.findByRole("heading", { level: 1, name: "Library" })
    await waitFor(() => expect(document.title).toBe("Library · Books"))
  })
})
