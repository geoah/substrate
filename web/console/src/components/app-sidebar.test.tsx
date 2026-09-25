// @vitest-environment jsdom
/** The sidebar's collection groups: everyday mode lists a group's primary
 * collections by display plural; technical mode lists the authority /
 * package tree with each kind's own name and can show the supporting ones;
 * provider groups start folded; favorites and folds persist on the console
 * preference record.
 *
 * The group components are rendered on their own rather than through
 * `AppSidebar`: the whole sidebar pulls the kind registry, the repository and
 * the bundle statuses over the network, and none of them says anything about
 * a group's shape. The router is a real one, because `Link` builds its href
 * from the route tree. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest"

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

import type { KindInfo } from "@/lib/api/types"
import { collectionGroups } from "@/lib/collections"
import { CollectionGroupNav, Favorites } from "./app-sidebar"
import { NavigationProvider } from "./console-preferences"

function kind(identity: string, purpose?: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { properties: {}, ...(purpose && { purpose }) },
  }
}

const [yours, google] = collectionGroups(
  [
    kind("ada.example.com/tasks/task"),
    kind("ada.example.com/tasks/project"),
    kind("ada.example.com/tasks/tasklog", "supporting"),
    kind("ada.example.com/people/person"),
    kind("providers.substrate.reamde.dev/google/contact"),
  ],
  "ada.example.com"
)

let storedPreferences: Record<string, unknown> = {}

beforeEach(() => {
  storedPreferences = {}
  localStorage.clear()
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT")
        storedPreferences = JSON.parse(String(init.body)).properties
      return new Response(
        JSON.stringify({
          id: "navigation",
          kind: "substrate.reamde.dev/core/consolepreference",
          version: 1,
          properties: storedPreferences,
        }),
        { status: 200 }
      )
    })
  )
})

function renderGroups() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <NavigationProvider>
          <Favorites />
          <CollectionGroupNav group={yours} />
          <CollectionGroupNav group={google} />
        </NavigationProvider>
      </QueryClientProvider>
    ),
  })
  // The paths the links spell have to exist, or the router has no href to
  // build. Their components are never reached: the memory history stays at the
  // root, which renders the groups under test.
  const routeTree = rootRoute.addChildren(
    [
      "/data/$authority",
      "/data/$authority/$pkg",
      "/data/$authority/$pkg/$name",
    ].map((path) =>
      createRoute({
        getParentRoute: () => rootRoute,
        path,
        component: () => null,
      })
    )
  )
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  // The app registers ITS router type globally, so `RouterProvider` asks for
  // that one router; this test tree is a different router of the same shape.
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

async function href(name: string): Promise<string | null> {
  return (await screen.findByRole("link", { name })).getAttribute("href")
}

async function press(label: string | RegExp) {
  const button = await screen.findByRole("button", { name: label })
  await waitFor(() =>
    expect((button as HTMLButtonElement).disabled).toBe(false)
  )
  fireEvent.click(button)
}

beforeAll(() => {
  // `SidebarProvider` asks whether the viewport is a phone; jsdom implements no
  // media queries, and a desktop answer is the one this tree is about.
  window.matchMedia = (media: string) =>
    ({
      media,
      matches: false,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("everyday groups", () => {
  it("lists a group's primary collections by display plural", async () => {
    renderGroups()
    expect(await href("Tasks")).toBe("/data/ada.example.com/tasks/task")
    expect(await href("People")).toBe("/data/ada.example.com/people/person")
    expect(screen.queryByRole("link", { name: "Task logs" })).toBeNull()
  })

  it("starts a provider's group folded and remembers opening it", async () => {
    renderGroups()
    await screen.findByRole("link", { name: "Tasks" })
    expect(screen.queryByRole("link", { name: "Contacts" })).toBeNull()
    await press(/^From Google/)
    expect(await href("Contacts")).toBe(
      "/data/providers.substrate.reamde.dev/google/contact"
    )
    await waitFor(() =>
      expect(storedPreferences.collapsed).toEqual([
        "group:provider:google:open",
      ])
    )
  })

  it("restores a folded group and reordered favorites in a new session", async () => {
    const first = renderGroups()
    const task = "ada.example.com/tasks/task"
    const person = "ada.example.com/people/person"
    await press(`Star ${task}`)
    await waitFor(() => expect(storedPreferences.favorites).toEqual([task]))
    await press(`Star ${person}`)
    await waitFor(() =>
      expect(storedPreferences.favorites).toEqual([task, person])
    )
    await press(`Move ${person} up`)
    await waitFor(() =>
      expect(storedPreferences.favorites).toEqual([person, task])
    )
    await press(/^Your data/)
    await waitFor(() =>
      expect(storedPreferences.collapsed).toEqual(["group:yours"])
    )
    first.unmount()
    renderGroups()
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: `Move ${person} up`,
          }) as HTMLButtonElement
        ).disabled
      ).toBe(true)
    )
    // The folded group lists nothing; the favorite still reaches its kind.
    expect(screen.queryByRole("link", { name: "Projects" })).toBeNull()
    expect(screen.getByRole("link", { name: task })).toBeTruthy()
  })
})

describe("technical groups", () => {
  beforeEach(() => {
    localStorage.setItem("substrate.console.technicalDetails", "true")
  })

  it("lists the authority and package tree, linked to their pages", async () => {
    renderGroups()
    expect(await href("ada.example.com")).toBe("/data/ada.example.com")
    expect(await href("tasks")).toBe("/data/ada.example.com/tasks")
    expect(await href("task")).toBe("/data/ada.example.com/tasks/task")
  })

  it("shows the supporting kinds on request, tagged", async () => {
    renderGroups()
    await screen.findByRole("link", { name: "task" })
    expect(screen.queryByRole("link", { name: /tasklog/ })).toBeNull()
    await press("Show 1 supporting and internal")
    const tasklog = await screen.findByRole("link", { name: /tasklog/ })
    expect(tasklog.textContent).toContain("supporting")
    expect(
      screen.getByRole("button", { name: "Hide 1 supporting and internal" })
    ).toBeTruthy()
  })
})
