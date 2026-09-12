// @vitest-environment jsdom
/** The Data tree's navigation contract: an authority row reaches the
 * authority's kinds table, a package row reaches the same table filtered to
 * the package, and the chevron beside each is the only thing that folds the
 * level under it.
 *
 * The two group components are rendered on their own rather than through
 * `AppSidebar`: the whole sidebar pulls the kind registry and the catalog over
 * the network, and neither answer says anything about the tree's shape. The
 * router is a real one, because `Link` builds its href from the route tree. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it } from "vitest"

import { buildKindNav } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import { AuthorityGroup } from "./app-sidebar"
import { SidebarMenu, SidebarProvider } from "./ui/sidebar"

function kind(pkg: string, name: string): KindInfo {
  return {
    identity: `ada.example.com/${pkg}/${name}`,
    name,
    authority: "ada.example.com",
    package: pkg,
    version: 1,
    source: "declared",
    description: "",
    definition: { properties: {} },
  }
}

const nav = buildKindNav([
  kind("tasks", "task"),
  kind("tasks", "project"),
  kind("people", "person"),
]).authorities[0]

function renderTree() {
  const rootRoute = createRootRoute({
    component: () => (
      <SidebarProvider>
        <SidebarMenu>
          <AuthorityGroup nav={nav} />
        </SidebarMenu>
      </SidebarProvider>
    ),
  })
  // The paths the links spell have to exist, or the router has no href to
  // build. Their components are never reached: the memory history stays at the
  // root, which renders the tree under test.
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/data/$authority",
      component: () => null,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/data/$authority/$pkg",
      component: () => null,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/data/$authority/$pkg/$name",
      component: () => null,
    }),
  ])
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

afterEach(cleanup)

describe("the Data tree", () => {
  it("links the authority's own label to its kinds table", async () => {
    renderTree()
    expect(await href("ada.example.com")).toBe("/data/ada.example.com")
  })

  it("links a package's label to its filtered table", async () => {
    renderTree()
    expect(await href("tasks")).toBe("/data/ada.example.com/tasks")
  })

  it("folds and unfolds one package's kinds from its chevron alone", async () => {
    renderTree()
    expect(await href("task")).toBe("/data/ada.example.com/tasks/task")
    expect(screen.getByRole("link", { name: "person" })).toBeDefined()

    const chevron = screen.getByRole("button", {
      name: "Toggle the kinds in tasks",
    })
    fireEvent.click(chevron)
    expect(screen.queryByRole("link", { name: "task" })).toBeNull()
    // Only the package the chevron belongs to folds.
    expect(screen.getByRole("link", { name: "person" })).toBeDefined()
    // The package's own row stays reachable while its kinds are hidden.
    expect(screen.getByRole("link", { name: "tasks" })).toBeDefined()

    fireEvent.click(chevron)
    expect(screen.getByRole("link", { name: "task" })).toBeDefined()
  })
})
