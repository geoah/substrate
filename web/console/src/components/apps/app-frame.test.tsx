// @vitest-environment jsdom
/** The frame's gate: the guest is mounted (the `substrate/mount` post with
 * the runtime, the source, the modules, the SDK major, the digest and a port)
 * only when the app's single-record read shows `source`, `modules` and
 * `permissions` at owner tier, and only for the shell the host itself
 * navigated to; below that the frame is not there, the review line is, and
 * nothing is posted. */

import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { appSpec } from "@/lib/apps/app-spec"
import { MOUNT_TYPE, READY_TYPE } from "@/lib/apps/bridge/protocol"
import { digest } from "@/lib/apps/digest"
import { AppFrame } from "./app-frame"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const SOURCE = "export default function Tasks() { return null }"
const MODULES = { rows: "export const n = 1" }

const taskKind: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      name: { type: "string" },
      status: { type: "state", states: ["open", "done"] },
    },
  },
}

const owner = { manager: "substratectl", tier: "owner" as const }

function appRecord(permissionsTier: "owner" | "machine"): SubstrateRecord {
  return {
    id: "tasks",
    kind: "substrate.reamde.dev/core/app",
    properties: {
      name: "Tasks",
      description: "Open tasks, add one, mark done.",
      runtime: "react",
      source: SOURCE,
      modules: MODULES,
      permissions: {
        reads: { kinds: [{ ref: `substrate.reamde.dev/core/kind/${TASK}` }] },
        writes: [{ ref: `substrate.reamde.dev/core/kind/${TASK}` }],
      },
    },
    labels: {},
    version: 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
    propertyMeta: {
      name: owner,
      runtime: owner,
      source: owner,
      modules: owner,
      permissions: { ...owner, tier: permissionsTier },
    },
  }
}

function mount(record: SubstrateRecord) {
  const spec = appSpec(record, [taskKind])
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => (
      <AppFrame
        spec={spec}
        record={record}
        kinds={[taskKind]}
        mode="page"
        route="/website"
      />
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { spec }
}

afterEach(cleanup)

describe("the frame's gate", () => {
  it("mounts the guest at owner provenance: the code and a port, posted once the shell is ready", async () => {
    mount(appRecord("owner"))
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer")
    expect(frame.src).toContain("/app-frame.html")
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted

    // A ready message from another window is not the shell's.
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: window,
        })
      )
    })
    expect(posted).not.toHaveBeenCalled()

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: frame.contentWindow,
        })
      )
    })
    // The digest is computed before the post, so the post is a beat away.
    await waitFor(() => expect(posted).toHaveBeenCalledTimes(1))
    const [message, target, transfer] = posted.mock.calls[0] as [
      Record<string, unknown>,
      string,
      MessagePort[],
    ]
    expect(message).toEqual({
      type: MOUNT_TYPE,
      runtime: "react",
      source: SOURCE,
      modules: MODULES,
      sdk: 1,
      digest: await digest(SOURCE, MODULES),
    })
    expect(target).toBe("*")
    expect(transfer[0]).toBeInstanceOf(MessagePort)

    // The post is armed once per navigation the host made: a second ready,
    // as from a document the guest navigated its frame to, gets nothing.
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: frame.contentWindow,
        })
      )
    })
    expect(posted).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/needs the owner's review/)).toBeNull()
  })

  it("withholds the mount below owner provenance and says the app needs review", async () => {
    mount(appRecord("machine"))
    expect(await screen.findByText(/needs the owner's review/)).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.getByText(/Open tasks, add one, mark done/)).toBeTruthy()
    expect(
      screen.getByRole("link", { name: "Open record" }).getAttribute("href")
    ).toContain("/data/substrate.reamde.dev/core/app/tasks")
  })
})
