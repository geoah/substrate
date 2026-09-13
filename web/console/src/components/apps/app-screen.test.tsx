// @vitest-environment jsdom
/** The app screen behind the shared gate. Nothing mounts until the record,
 * the registry and the package versions have all landed, and a
 * `requiresAtLeast` floor the repository is below is refused where the frame
 * would be. The header's back is put to the guest first and the host moves
 * only for a guest that did not take it, the app's history while the splat
 * is non-empty and the launcher at the root. An input whose kind the grant
 * does not read is explained under the header and never queried, and the
 * guest is told nothing of it. */

import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useParams,
} from "@tanstack/react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  Endpoint,
  METHODS,
  READY_TYPE,
  type InitializeResult,
} from "@/lib/apps/bridge/protocol"
import { AppScreen } from "./app-screen"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const PERSON = "ada.example.com/people/person"
const owner = { manager: "substratectl", tier: "owner" as const }
const machine = { manager: "agent", tier: "machine" as const }
const kindRef = (identity: string) => ({
  ref: `substrate.reamde.dev/core/kind/${identity}`,
})

const taskKind: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: { properties: { name: { type: "string" } } },
}
const personKind: KindInfo = {
  ...taskKind,
  identity: PERSON,
  name: "person",
  package: "people",
  definition: { properties: {} },
}

function appRecord(over: Record<string, unknown> = {}): SubstrateRecord {
  return {
    id: "tasks",
    kind: "substrate.reamde.dev/core/app",
    properties: {
      name: "Tasks",
      runtime: "react",
      source: "export default function Tasks() { return null }",
      permissions: { reads: { kinds: [kindRef(TASK)] } },
      ...over,
    },
    labels: {},
    version: 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
    propertyMeta: {
      name: owner,
      runtime: owner,
      source: owner,
      permissions: owner,
      inputs: machine,
      requiresAtLeast: owner,
    },
  }
}

const packageRow = (id: string, version: number): SubstrateRecord => ({
  id,
  kind: "substrate.reamde.dev/core/package",
  properties: { version },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200 })

describe("AppScreen", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
    window.matchMedia = (media: string) =>
      ({
        matches: false,
        media,
        onchange: null,
        addEventListener() {},
        removeEventListener() {},
        addListener() {},
        removeListener() {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  /** The wire: the registry, the app's single-record read, the package
   * collection (deferrable, so the gate's wait can be watched) and every
   * other collection empty. */
  function serve(
    app: SubstrateRecord,
    packages: SubstrateRecord[] | Promise<SubstrateRecord[]>
  ) {
    fetchMock.mockImplementation(async (url) => {
      const path = new URL(String(url), "http://console").pathname
      if (path === "/api/v1/substrate.reamde.dev/core/kind") {
        return json({ kinds: [taskKind, personKind], head: 1, generation: "g" })
      }
      if (path === "/api/v1/substrate.reamde.dev/core/app/tasks") {
        return json(app)
      }
      if (path === "/api/v1/substrate.reamde.dev/core/package") {
        return json({ records: await packages, head: 1, generation: "g" })
      }
      return json({ records: [], head: 1, generation: "g" })
    })
  }

  const asked = () =>
    fetchMock.mock.calls.map(
      (c) => new URL(String(c[0]), "http://console").pathname
    )

  function AppPage() {
    const { id = "", _splat } = useParams({ strict: false })
    return <AppScreen id={id} splat={_splat ?? ""} />
  }

  function mount(entries: string[]) {
    const rootRoute = createRootRoute({ component: () => <Outlet /> })
    const appsRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps",
      component: () => <div>launcher</div>,
    })
    const appRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$id",
      component: AppPage,
    })
    const splatRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$id/$",
      component: AppPage,
    })
    const router = createRouter({
      routeTree: rootRoute.addChildren([appsRoute, appRoute, splatRoute]),
      history: createMemoryHistory({ initialEntries: entries }),
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <Toaster>
          <RouterProvider router={router} />
        </Toaster>
      </QueryClientProvider>
    )
    return {
      pathname: () => router.state.location.pathname,
      canGoBack: () => router.history.canGoBack(),
    }
  }

  /** The shell's side of the handshake, minus the SDK. */
  async function handshake(onRequest: (method: string) => unknown) {
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: frame.contentWindow,
        })
      )
    })
    await waitFor(() => expect(posted).toHaveBeenCalledTimes(1))
    const [, , transfer] = posted.mock.calls[0] as [
      unknown,
      string,
      MessagePort[],
    ]
    const rpc = new Endpoint(transfer[0], {
      onRequest: (method) => onRequest(method),
      onNotification: () => {},
    })
    const init = await rpc.call<InitializeResult>(METHODS.initialize, {})
    rpc.notify(METHODS.initialized)
    return { rpc, init }
  }

  const settle = () => new Promise((r) => setTimeout(r, 40))

  it("mounts nothing until the package versions land, then refuses a floor the repository is below", async () => {
    let land: (rows: SubstrateRecord[]) => void = () => {}
    const packages = new Promise<SubstrateRecord[]>((r) => (land = r))
    serve(
      appRecord({ requiresAtLeast: { "ada.example.com/tasks": 5 } }),
      packages
    )
    mount(["/apps/tasks"])
    await settle()
    expect(asked()).toContain("/api/v1/substrate.reamde.dev/core/package")
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.queryByText(/needs a newer package/)).toBeNull()

    land([packageRow("ada.example.com/tasks", 3)])
    expect(
      await screen.findByText("This app needs a newer package")
    ).toBeTruthy()
    expect(
      screen.getByText(
        /holds ada.example.com\/tasks at version 3; the app needs 5/
      )
    ).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
  })

  it("puts the header's back to the guest first, and moves the host only when it is not handled", async () => {
    serve(appRecord(), [packageRow("ada.example.com/tasks", 3)])
    const { pathname, canGoBack } = mount(["/apps/tasks", "/apps/tasks/detail"])
    let handles = true
    const g = await handshake((method) =>
      method === METHODS.back ? { handled: handles } : {}
    )
    expect(g.init.hostContext.app.route).toBe("/detail")

    const back = () =>
      act(() => screen.getByRole("button", { name: "Back" }).click())
    back()
    await settle()
    expect(pathname()).toBe("/apps/tasks/detail")

    // Unhandled, with an entry of ours behind: the app's history is walked.
    handles = false
    back()
    await waitFor(() => expect(pathname()).toBe("/apps/tasks"))
    expect(canGoBack()).toBe(false)
    g.rpc.close()

    // At the app's root, with no guest up to ask, the host opens the launcher.
    await screen.findByTitle("Tasks")
    back()
    await waitFor(() => expect(pathname()).toBe("/apps"))
  })

  it("lands a deep link opened straight on the app's root, not on whatever came before the console", async () => {
    serve(appRecord(), [packageRow("ada.example.com/tasks", 3)])
    const straight = mount(["/apps/tasks/detail"])
    const g = await handshake(() => ({ handled: false }))
    expect(straight.canGoBack()).toBe(false)
    act(() => screen.getByRole("button", { name: "Back" }).click())
    await waitFor(() => expect(straight.pathname()).toBe("/apps/tasks"))
    // Pushed, not walked: there was nothing to walk to.
    expect(straight.canGoBack()).toBe(true)
    g.rpc.close()
  })

  it("explains an input outside the grant under the header, never reads it, and tells the guest nothing of it", async () => {
    // Owner-written source and grant (tasks); a machine-written input naming
    // people, which the grant does not read.
    serve(appRecord({ inputs: { me: { kind: kindRef(PERSON) } } }), [
      packageRow("ada.example.com/tasks", 3),
    ])
    mount(["/apps/tasks"])
    expect(
      await screen.findByText(
        /`me` reads ada.example.com\/people\/person, which is outside the app's grant/
      )
    ).toBeTruthy()
    const g = await handshake(() => ({}))
    expect(g.init.hostContext.app.inputs).toEqual({})
    expect(asked()).not.toContain(`/api/v1/${PERSON}`)
    g.rpc.close()
  })
})
