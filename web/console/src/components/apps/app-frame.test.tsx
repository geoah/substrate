// @vitest-environment jsdom
/** The frame's gate and its door. The guest is mounted (the `substrate/mount`
 * post with the runtime, the source, the modules, the SDK major, the digest
 * and a port) only when the app's single-record read shows `source`,
 * `modules` and `permissions` at owner tier, and only for the shell the host
 * itself navigated to; below that the frame is not there, the review line
 * is, and nothing is posted. Over the port, the guest is served a complete
 * app context and hears it again, whole, whenever an input, a binding or a
 * declaration moves; an input whose kind the grant does not read is
 * withheld whatever the resolver said; and the host's back is put to the
 * guest first, the fallback root-aware. */

import { createRef, useMemo, useState } from "react"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
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
import {
  Endpoint,
  LEFT_TYPE,
  METHODS,
  MOUNT_TYPE,
  notification,
  READY_TYPE,
  type MountMessage,
  type HostContext,
  type InitializeResult,
  type JsonRpcNotification,
} from "@/lib/apps/bridge/protocol"
import { digest } from "@/lib/apps/digest"
import type { ResolvedInputState } from "@/lib/apps/inputs"
import { AppFrame, type AppFrameHandle } from "./app-frame"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const PERSON = "ada.example.com/people/person"
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
const personKind: KindInfo = {
  ...taskKind,
  identity: PERSON,
  name: "person",
  package: "people",
  definition: { properties: {} },
}

const owner = { manager: "substratectl", tier: "owner" as const }
const machine = { manager: "agent", tier: "machine" as const }

const kindRef = (identity: string) => ({
  ref: `substrate.reamde.dev/core/kind/${identity}`,
})

function appRecord(
  permissionsTier: "owner" | "machine",
  over: Record<string, unknown> = {}
): SubstrateRecord {
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
        reads: { kinds: [kindRef(TASK)] },
        writes: [kindRef(TASK)],
      },
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
      modules: owner,
      permissions: { ...owner, tier: permissionsTier },
      inputs: machine,
    },
  }
}

const task = (id: string): SubstrateRecord => ({
  id,
  kind: TASK,
  properties: { name: id },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

interface Props {
  record: SubstrateRecord
  kinds: KindInfo[]
  route: string
  inputs: Record<string, ResolvedInputState>
  onBack?: () => void
}

/** The frame under a memory router, its props movable from the test, with a
 * launcher route and one more page so both of back's fallbacks have
 * somewhere to land. */
function mount(
  initial: Partial<Props> & { record: SubstrateRecord },
  at = "/"
) {
  const handle = createRef<AppFrameHandle>()
  let update: (p: Partial<Props>) => void = () => {}
  function Screen() {
    const [props, set] = useState<Props>({
      kinds: [taskKind, personKind],
      route: "",
      inputs: {},
      ...initial,
    })
    update = (p) => set((prev) => ({ ...prev, ...p }))
    const spec = useMemo(
      () => appSpec(props.record, props.kinds),
      [props.record, props.kinds]
    )
    return (
      <AppFrame
        ref={handle}
        spec={spec}
        record={props.record}
        kinds={props.kinds}
        mode="page"
        route={props.route}
        inputs={props.inputs}
        onBack={props.onBack}
      />
    )
  }
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: Screen,
  })
  const appsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/apps",
    component: () => <div>launcher</div>,
  })
  const appRootRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/apps/$id",
    component: () => <div>app root</div>,
  })
  const elsewhereRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/elsewhere",
    component: () => <div>elsewhere</div>,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      indexRoute,
      appsRoute,
      appRootRoute,
      elsewhereRoute,
    ]),
    history: createMemoryHistory({
      initialEntries: at === "/" ? ["/"] : [at, "/"],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return {
    handle,
    router,
    update: (p: Partial<Props>) => act(() => update(p)),
    pathname: () => router.state.location.pathname,
  }
}

const ready = (source: Window | null) =>
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", { data: { type: READY_TYPE }, source })
    )
  })

/** The shell's side of the handshake, minus the SDK: answer the mount with
 * `ui/initialize` over the port it carried, and keep every notification. */
async function handshake(
  frame: HTMLIFrameElement,
  onRequest: (method: string) => unknown = () => ({})
) {
  const posted = vi.fn()
  frame.contentWindow!.postMessage = posted
  ready(frame.contentWindow)
  await waitFor(() => expect(posted).toHaveBeenCalledTimes(1))
  const [, , transfer] = posted.mock.calls[0] as [
    unknown,
    string,
    MessagePort[],
  ]
  const notices: JsonRpcNotification[] = []
  const rpc = new Endpoint(transfer[0], {
    onRequest: (method) => onRequest(method),
    onNotification: (method, params) =>
      notices.push(notification(method, params)),
  })
  const init = await rpc.call<InitializeResult>(METHODS.initialize, {})
  rpc.notify(METHODS.initialized)
  const contexts = () =>
    notices
      .filter((n) => n.method === METHODS.hostContextChanged)
      .map((n) => n.params as Partial<HostContext>)
  return { rpc, notices, init, contexts, posted }
}

const settle = () => new Promise((r) => setTimeout(r, 30))

afterEach(cleanup)

describe("the frame's gate", () => {
  it("mounts the guest at owner provenance: the code and a port, posted once the shell is ready", async () => {
    mount({ record: appRecord("owner") })
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer")
    expect(frame.src).toContain("/app-frame.html")
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted

    // A ready message from another window is not the shell's.
    ready(window)
    expect(posted).not.toHaveBeenCalled()

    ready(frame.contentWindow)
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
      nonce: expect.any(String),
    })
    expect(target).toBe("*")
    expect(transfer[0]).toBeInstanceOf(MessagePort)

    // The post is armed once per navigation the host made: a second ready,
    // as from a document the guest navigated its frame to, gets nothing.
    ready(frame.contentWindow)
    expect(posted).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/needs the owner's review/)).toBeNull()
  })

  it("closes the view when the shell reports its document unloading: the guest navigated its own frame", async () => {
    mount({ record: appRecord("owner") })
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted
    ready(frame.contentWindow)
    await waitFor(() => expect(posted).toHaveBeenCalledTimes(1))
    const { nonce } = posted.mock.calls[0][0] as MountMessage
    // The message is posted as the document unloads, so it has no source;
    // the mount's nonce is what names this frame.
    const left = (n: string) =>
      act(() => {
        window.dispatchEvent(
          new MessageEvent("message", {
            data: { type: LEFT_TYPE, nonce: n },
            source: null,
          })
        )
      })
    left("not-this-mount")
    expect(screen.queryByText(/left its document/)).toBeNull()
    expect(document.querySelector("iframe")).toBe(frame)

    left(nonce)
    expect(
      await screen.findByText(/The app left its document and was closed/)
    ).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
    // Reload is a fresh shell, navigated by the host again.
    fireEvent.click(screen.getByRole("button", { name: "Reload" }))
    const again = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    expect(again).not.toBe(frame)
    expect(again.src).toContain("/app-frame.html")
  })

  it("withholds the mount below owner provenance and says the app needs review", async () => {
    mount({ record: appRecord("machine") })
    expect(await screen.findByText(/needs the owner's review/)).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.getByText(/Open tasks, add one, mark done/)).toBeTruthy()
    expect(
      screen.getByRole("link", { name: "Open record" }).getAttribute("href")
    ).toContain("/data/substrate.reamde.dev/core/app/tasks")
  })
})

describe("the frame's door", () => {
  const withInput = appRecord("owner", {
    inputs: { me: { kind: kindRef(TASK) } },
  })

  it("serves a complete app context and pushes it whole when an input settles, a binding changes or a declaration lands", async () => {
    const { update } = mount({
      record: withInput,
      inputs: { me: { state: "loading" } },
    })
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const g = await handshake(frame)
    expect(g.init.hostContext.app).toEqual({
      id: "tasks",
      name: "Tasks",
      route: "",
      inputs: { me: { state: "loading" } },
    })
    expect(g.init.hostContext.kinds.map((k) => k.identity)).toEqual([TASK])

    // The input's query settles after the guest came up.
    update({ inputs: { me: { state: "sole", record: task("t1") } } })
    await waitFor(() => expect(g.contexts()).toHaveLength(1))
    expect(g.contexts()[0]).toEqual({
      app: {
        id: "tasks",
        name: "Tasks",
        route: "",
        inputs: { me: { state: "sole", record: task("t1") } },
      },
      kinds: [taskKind],
    })

    // The owner binds another record in Settings.
    update({ inputs: { me: { state: "bound", record: task("t2") } } })
    await waitFor(() => expect(g.contexts()).toHaveLength(2))
    expect(g.contexts()[1].app?.inputs.me).toEqual({
      state: "bound",
      record: task("t2"),
    })

    // The same state in a new object is not news.
    update({ inputs: { me: { state: "bound", record: task("t2") } } })
    await settle()
    expect(g.contexts()).toHaveLength(2)

    // A declaration lands: the guest hears the current kinds.
    update({ kinds: [{ ...taskKind, version: 2 }, personKind] })
    await waitFor(() => expect(g.contexts()).toHaveLength(3))
    expect(g.contexts()[2].kinds?.map((k) => k.version)).toEqual([2])
    g.rpc.close()
  })

  it("withholds an input whose kind the grant does not read, whatever the resolver said", async () => {
    const record = appRecord("owner", {
      inputs: { me: { kind: kindRef(TASK) }, other: { kind: kindRef(PERSON) } },
    })
    const { update } = mount({
      record,
      inputs: {
        me: { state: "sole", record: task("t1") },
        // A resolver that was handed a person: the door still drops it.
        other: { state: "sole", record: { ...task("p1"), kind: PERSON } },
        stray: { state: "ungranted", kind: PERSON },
      },
    })
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const g = await handshake(frame)
    expect(Object.keys(g.init.hostContext.app.inputs)).toEqual(["me"])
    update({
      inputs: {
        me: { state: "sole", record: task("t2") },
        other: { state: "sole", record: { ...task("p2"), kind: PERSON } },
      },
    })
    await waitFor(() => expect(g.contexts()).toHaveLength(1))
    expect(Object.keys(g.contexts()[0].app!.inputs)).toEqual(["me"])
    g.rpc.close()
  })

  it("puts the header's back to the guest first, and gives the SDK's back the root-aware fallback", async () => {
    let handles = true
    const { handle, pathname } = mount(
      { record: appRecord("owner") },
      "/elsewhere"
    )
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const g = await handshake(frame, (method) =>
      method === METHODS.back ? { handled: handles } : {}
    )
    await expect(handle.current!.back()).resolves.toBe(true)
    handles = false
    await expect(handle.current!.back()).resolves.toBe(false)
    expect(pathname()).toBe("/")

    // The SDK's `host.back()` at the app's root opens the launcher.
    await g.rpc.call(METHODS.navigate, { back: true })
    await waitFor(() => expect(pathname()).toBe("/apps"))
    g.rpc.close()
  })

  it("walks the app's own history for the SDK's back while its route is non-empty, and defers to a mount that owns history", async () => {
    const deep = mount(
      { record: appRecord("owner"), route: "/website" },
      "/elsewhere"
    )
    const frame = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const g = await handshake(frame)
    await g.rpc.call(METHODS.navigate, { back: true })
    await waitFor(() => expect(deep.pathname()).toBe("/elsewhere"))
    g.rpc.close()
    cleanup()

    // Opened straight at a path, with nothing of ours behind it: the app's
    // root, never whatever page came before the console.
    const straight = mount({ record: appRecord("owner"), route: "/website" })
    const third = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const s = await handshake(third)
    await s.rpc.call(METHODS.navigate, { back: true })
    await waitFor(() => expect(straight.pathname()).toBe("/apps/tasks"))
    s.rpc.close()
    cleanup()

    const onBack = vi.fn()
    const owned = mount({ record: appRecord("owner"), onBack }, "/elsewhere")
    const second = (await screen.findByTitle("Tasks")) as HTMLIFrameElement
    const h = await handshake(second)
    await h.rpc.call(METHODS.navigate, { back: true })
    expect(onBack).toHaveBeenCalledTimes(1)
    expect(owned.pathname()).toBe("/")
    h.rpc.close()
  })
})
