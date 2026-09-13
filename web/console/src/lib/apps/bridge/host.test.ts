/** The bridge on the console side: the JSON-RPC framing both ends share, the
 * grant every call goes through, and the host's own behaviour over a port:
 * the page is pushed once the guest is ready and only under a grant the
 * owner wrote, a call outside the grant is refused with `forbidden`, and a
 * guest that stops answering pings is torn down. */

import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import type { ViewSpec } from "../spec"
import {
  checkAccess,
  createBridgeHost,
  NOT_GRANTED,
  ownerProvenance,
  PING_MISSES,
  type Provenance,
  type RecordsApi,
} from "./host"
import {
  Endpoint,
  ERROR,
  METHODS,
  notification,
  parseMessage,
  request,
  RpcError,
  TOOLS,
  type CallToolResult,
  type HostContext,
  type InitializeResult,
  type JsonRpcNotification,
} from "./protocol"

const TASK = "ada.example.com/tasks/task"
const PERSON = "ada.example.com/people/person"

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
      title: { type: "string" },
      status: {
        type: "state",
        states: ["todo", "doing", "done"],
        transitions: [
          { from: "todo", to: "doing" },
          { from: "doing", to: "done" },
        ],
      },
    },
  },
}

const personKind: KindInfo = {
  ...taskKind,
  identity: PERSON,
  name: "person",
  package: "people",
}
const kinds = [taskKind, personKind]

function spec(over: Partial<ViewSpec["permissions"]> = {}): ViewSpec {
  return {
    id: "heat",
    name: "Heat",
    layout: "custom",
    kind: TASK,
    requiresAtLeast: {},
    filter: {},
    orderBy: [],
    show: [],
    facets: [],
    window: {},
    first: 50,
    related: [],
    attach: [],
    replaces: false,
    actions: [],
    permissions: {
      reads: { kinds: [TASK] },
      writes: [],
      call: [],
      agents: [],
      ...over,
    },
    source: "<p>hi</p>",
    problems: [],
  }
}

function record(id: string, status = "todo"): SubstrateRecord {
  return {
    id,
    kind: TASK,
    properties: { title: id, status },
    labels: {},
    version: 3,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const page: Page = {
  records: [record("t1", "done"), record("t2", "done")],
  head: 42,
  generation: "g",
}

const hostContext: HostContext = {
  theme: "light",
  displayMode: "inline",
  containerDimensions: { width: 300, height: 200 },
  safeAreaInsets: { top: 0, right: 0, bottom: 0, left: 0 },
  platform: "web",
  locale: "en",
  timeZone: "UTC",
  styles: { variables: { "--primary": "oklch(0.5 0.1 235)" }, fontFaces: [] },
}

/** A port pair that delivers on the microtask queue, so a test drives the
 * bridge with `await` alone and fake timers never hold a message back. */
function pair(): [MessagePort, MessagePort] {
  const make = () =>
    ({
      onmessage: null,
      postMessage() {},
      close() {},
      start() {},
    }) as unknown as MessagePort & {
      onmessage: ((e: MessageEvent) => void) | null
    }
  const a = make()
  const b = make()
  const link = (from: typeof a, to: typeof b) => {
    from.postMessage = (data: unknown) => {
      queueMicrotask(() => to.onmessage?.({ data } as MessageEvent))
    }
  }
  link(a, b)
  link(b, a)
  return [a, b]
}

async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
}

function fakeApi(): RecordsApi & { calls: string[] } {
  const calls: string[] = []
  return {
    calls,
    list: async (p) => {
      calls.push(`list ${p.name}`)
      return page
    },
    get: async (_k, id) => {
      calls.push(`get ${id}`)
      return record(id, "doing")
    },
    create: async () => record("new"),
    put: async (_k, id) => record(id),
    patch: async (_k, id, patch) => {
      calls.push(`patch ${id} ${JSON.stringify(patch.properties)}`)
      return record(id, "done")
    },
    delete: async (_k, id) => {
      calls.push(`delete ${id}`)
    },
  }
}

interface Guest {
  rpc: Endpoint
  notices: JsonRpcNotification[]
  init(): Promise<InitializeResult>
  call(name: string, args: Record<string, unknown>): Promise<CallToolResult>
}

/** The guest's side of the port, minus the SDK: a bare endpoint that records
 * what the host sends and answers pings unless told not to. */
function guest(port: MessagePort, opts: { answerPings?: boolean } = {}): Guest {
  const notices: JsonRpcNotification[] = []
  const rpc = new Endpoint(port, {
    onRequest(method) {
      if (method === METHODS.ping) {
        if (opts.answerPings === false) return new Promise(() => {})
        return {}
      }
      return {}
    },
    onNotification(method, params) {
      notices.push(notification(method, params))
    },
  })
  return {
    rpc,
    notices,
    async init() {
      const result = await rpc.call<InitializeResult>(METHODS.initialize, {})
      rpc.notify(METHODS.initialized)
      await settle()
      return result
    },
    call: (name, args) =>
      rpc.call<CallToolResult>(METHODS.toolsCall, { name, arguments: args }),
  }
}

const OWNERS: Provenance = { granted: true, filtered: true }

function mount(opts: {
  spec?: ViewSpec
  provenance?: Provenance
  api?: RecordsApi
  answerPings?: boolean
  onLost?: () => void
  onUnloaded?: () => void
  pingMs?: number
}) {
  const [hostPort, guestPort] = pair()
  const view = opts.spec ?? spec()
  const host = createBridgeHost({
    view: () => ({ spec: view, kinds }),
    provenance: () => opts.provenance ?? OWNERS,
    api: opts.api ?? fakeApi(),
    pingMs: opts.pingMs,
    callbacks: {
      hostContext: () => hostContext,
      onLost: opts.onLost,
      onUnloaded: opts.onUnloaded,
    },
  })
  host.attach(hostPort)
  return { host, guest: guest(guestPort, { answerPings: opts.answerPings }) }
}

afterEach(() => {
  vi.useRealTimers()
})

describe("framing", () => {
  it("frames the three shapes and drops the rest", () => {
    expect(parseMessage(request(1, "ping"))).toEqual({
      jsonrpc: "2.0",
      id: 1,
      method: "ping",
    })
    expect(parseMessage(notification("ui/notifications/initialized"))).toEqual({
      jsonrpc: "2.0",
      method: "ui/notifications/initialized",
    })
    expect(parseMessage({ jsonrpc: "2.0", id: "a", result: {} })).toBeTruthy()
    expect(
      parseMessage({ jsonrpc: "2.0", id: 2, error: { code: -1, message: "x" } })
    ).toBeTruthy()
    expect(parseMessage(JSON.stringify(request(3, "ping", { a: 1 })))).toEqual(
      request(3, "ping", { a: 1 })
    )
    expect(parseMessage({ id: 1, method: "ping" })).toBeUndefined()
    expect(parseMessage("not json")).toBeUndefined()
    expect(parseMessage(null)).toBeUndefined()
    expect(parseMessage({ jsonrpc: "2.0", id: 1 })).toBeUndefined()
  })

  it("answers a request, fails one with its code, and rejects the pending on close", async () => {
    const [a, b] = pair()
    const server = new Endpoint(a, {
      onRequest(method, params) {
        if (method === "add") {
          const { x, y } = params as { x: number; y: number }
          return { sum: x + y }
        }
        throw new RpcError(ERROR.methodNotFound, `no ${method}`)
      },
      onNotification() {},
    })
    const client = new Endpoint(b, {
      onRequest: () => ({}),
      onNotification() {},
    })
    await expect(client.call("add", { x: 2, y: 3 })).resolves.toEqual({
      sum: 5,
    })
    await expect(client.call("nope")).rejects.toMatchObject({
      code: ERROR.methodNotFound,
    })
    const hanging = client.call("add", { x: 1, y: 1 })
    client.close()
    await expect(hanging).rejects.toMatchObject({ code: ERROR.unavailable })
    server.close()
  })
})

describe("the grant", () => {
  it("passes a read of a declared kind", () => {
    const access = checkAccess(spec(), kinds, "read", TASK)
    expect(access.ok).toBe(true)
    if (access.ok) expect(access.kind.identity).toBe(TASK)
  })

  it("refuses a read of an undeclared kind", () => {
    expect(checkAccess(spec(), kinds, "read", PERSON)).toEqual({
      ok: false,
      message: `the view's grant does not read ${PERSON}`,
    })
  })

  it("refuses a kind the registry does not know, and a call naming none", () => {
    expect(
      checkAccess(spec(), kinds, "read", "ada.example.com/tasks/Task").ok
    ).toBe(false)
    expect(checkAccess(spec(), kinds, "read", `${TASK}/`).ok).toBe(false)
    expect(checkAccess(spec(), kinds, "read", undefined).ok).toBe(false)
    expect(checkAccess(spec(), kinds, "read", 7).ok).toBe(false)
  })

  it("refuses a write without a writes grant, and passes one with it", () => {
    expect(checkAccess(spec(), kinds, "write", TASK).ok).toBe(false)
    expect(checkAccess(spec({ writes: [TASK] }), kinds, "write", TASK).ok).toBe(
      true
    )
    expect(
      checkAccess(spec({ writes: [TASK] }), kinds, "write", PERSON).ok
    ).toBe(false)
  })
})

describe("owner provenance", () => {
  const row = (
    meta: Record<string, { tier?: string }>,
    props?: Record<string, unknown>
  ): SubstrateRecord => ({
    ...record("v"),
    properties: props ?? {
      source: "<p>",
      permissions: {},
      kind: { ref: "k" },
      filter: {},
    },
    propertyMeta: meta as SubstrateRecord["propertyMeta"],
  })
  const owner = { tier: "owner" }

  it("holds, page included, when all four are present and the owner's", () => {
    expect(
      ownerProvenance(
        row({ source: owner, permissions: owner, kind: owner, filter: owner })
      )
    ).toEqual({ granted: true, filtered: true })
  })

  it("holds without a page when the filter is absent: nobody approved a selection", () => {
    expect(
      ownerProvenance(
        row(
          { source: owner, permissions: owner, kind: owner },
          { source: "<p>", permissions: {}, kind: { ref: "k" } }
        )
      )
    ).toEqual({ granted: true, filtered: false })
  })

  it("fails when a required property is absent: a deletion is not an approval", () => {
    for (const missing of ["source", "permissions", "kind"] as const) {
      const props: Record<string, unknown> = {
        source: "<p>",
        permissions: {},
        kind: { ref: "k" },
        filter: {},
      }
      delete props[missing]
      expect(
        ownerProvenance(
          row(
            { source: owner, permissions: owner, kind: owner, filter: owner },
            props
          )
        )
      ).toEqual(NOT_GRANTED)
    }
  })

  it("fails when one is below owner, unrecorded, or the source is missing", () => {
    expect(
      ownerProvenance(
        row({
          source: owner,
          permissions: { tier: "machine" },
          kind: owner,
          filter: owner,
        })
      )
    ).toEqual(NOT_GRANTED)
    // A filter that is present but unrecorded is below owner, not absent.
    expect(
      ownerProvenance(row({ source: owner, permissions: owner, kind: owner }))
    ).toEqual(NOT_GRANTED)
    expect(ownerProvenance(row({}))).toEqual(NOT_GRANTED)
    expect(
      ownerProvenance(row({ source: owner }, { permissions: {} }))
    ).toEqual(NOT_GRANTED)
    expect(ownerProvenance(undefined)).toEqual(NOT_GRANTED)
  })
})

describe("the host over a port", () => {
  it("answers initialize with the host context and pushes the page once the guest is ready", async () => {
    const { host, guest: g } = mount({})
    host.pushPage(page)
    const init = await g.init()
    expect(init.hostContext).toEqual(hostContext)
    expect(init.protocolVersion).toBe("2026-01-26")
    const pushed = g.notices.find((n) => n.method === METHODS.toolResult)
    expect(pushed?.params).toEqual({
      content: [],
      structuredContent: { records: page.records, head: 42 },
    })
    host.teardown()
  })

  it("re-pushes when the page changes", async () => {
    const { host, guest: g } = mount({})
    await g.init()
    host.pushPage(page)
    host.pushPage({ ...page, records: [record("t3", "done")], head: 43 })
    await settle()
    const pushes = g.notices.filter((n) => n.method === METHODS.toolResult)
    expect(pushes).toHaveLength(2)
    expect((pushes[1].params as CallToolResult).structuredContent?.head).toBe(
      43
    )
    host.teardown()
  })

  it("serves a list of a declared kind and refuses one of an undeclared kind", async () => {
    const api = fakeApi()
    const { host, guest: g } = mount({ api })
    await g.init()
    const result = await g.call(TOOLS.list, { kind: TASK, first: 10 })
    expect(result.structuredContent).toEqual({
      records: page.records,
      cursor: undefined,
      head: 42,
    })
    await expect(g.call(TOOLS.list, { kind: PERSON })).rejects.toMatchObject({
      code: ERROR.forbidden,
      message: `the view's grant does not read ${PERSON}`,
    })
    expect(api.calls).toEqual(["list task"])
    host.teardown()
  })

  it("refuses every write without a writes grant and serves them with one", async () => {
    const api = fakeApi()
    const { host, guest: g } = mount({ api })
    await g.init()
    for (const [tool, args] of [
      [TOOLS.put, { kind: TASK, properties: { title: "x" } }],
      [TOOLS.patch, { kind: TASK, id: "t1", properties: { title: "x" } }],
      [TOOLS.delete, { kind: TASK, id: "t1" }],
      [TOOLS.transition, { kind: TASK, id: "t1", to: "done" }],
    ] as const) {
      await expect(g.call(tool, { ...args })).rejects.toMatchObject({
        code: ERROR.forbidden,
      })
    }
    expect(api.calls).toEqual([])
    host.teardown()

    const granted = mount({ api, spec: spec({ writes: [TASK] }) })
    await granted.guest.init()
    const moved = await granted.guest.call(TOOLS.transition, {
      kind: TASK,
      id: "t1",
      to: "done",
    })
    expect(
      (moved.structuredContent as unknown as SubstrateRecord).properties.status
    ).toBe("done")
    // The declared machine is checked against the row's current state
    // before the PATCH, and the CAS precondition is the version read.
    expect(api.calls).toEqual(["get t1", 'patch t1 {"status":"done"}'])
    const refused = await granted.guest.call(TOOLS.transition, {
      kind: TASK,
      id: "t1",
      to: "todo",
    })
    expect(refused.isError).toBe(true)
    expect(refused.content[0].text).toMatch(/does not move from doing to todo/)
    granted.host.teardown()
  })

  it("below owner provenance pushes nothing and refuses every call, whatever the row grants", async () => {
    const api = fakeApi()
    const { host, guest: g } = mount({
      api,
      provenance: NOT_GRANTED,
      spec: spec({ writes: [TASK] }),
    })
    host.pushPage(page)
    await g.init()
    await settle()
    expect(g.notices.some((n) => n.method === METHODS.toolResult)).toBe(false)
    await expect(g.call(TOOLS.list, { kind: TASK })).rejects.toMatchObject({
      code: ERROR.forbidden,
    })
    await expect(
      g.call(TOOLS.get, { kind: TASK, id: "t1" })
    ).rejects.toMatchObject({
      code: ERROR.forbidden,
    })
    expect(api.calls).toEqual([])
    host.teardown()
  })

  it("withholds the page when the grant does not read the view's own kind", async () => {
    const { host, guest: g } = mount({
      spec: spec({ reads: { kinds: [PERSON] } }),
    })
    host.pushPage(page)
    await g.init()
    expect(g.notices.some((n) => n.method === METHODS.toolResult)).toBe(false)
    host.teardown()
  })

  it("pushes no page without an owner-written filter, and still serves the grant's calls", async () => {
    const api = fakeApi()
    const { host, guest: g } = mount({
      api,
      provenance: { granted: true, filtered: false },
    })
    host.pushPage(page)
    await g.init()
    await settle()
    expect(g.notices.some((n) => n.method === METHODS.toolResult)).toBe(false)
    const listed = await g.call(TOOLS.list, { kind: TASK })
    expect(listed.structuredContent?.records).toEqual(page.records)
    await expect(g.call(TOOLS.list, { kind: PERSON })).rejects.toMatchObject({
      code: ERROR.forbidden,
    })
    expect(api.calls).toEqual(["list task"])
    host.teardown()
  })

  it("tears down when the shell says the document is leaving its frame", async () => {
    const onLost = vi.fn()
    const onUnloaded = vi.fn()
    const { host, guest: g } = mount({ onLost, onUnloaded })
    await g.init()
    g.rpc.notify(METHODS.unload)
    await settle()
    expect(onUnloaded).toHaveBeenCalledTimes(1)
    expect(onLost).not.toHaveBeenCalled()
    expect(host.closed).toBe(true)
    // Idempotent: the port is closed, a second notice reaches nothing.
    g.rpc.notify(METHODS.unload)
    await settle()
    expect(onUnloaded).toHaveBeenCalledTimes(1)
  })

  it("refuses a link off http, https and mailto, and unknown methods", async () => {
    const { host, guest: g } = mount({})
    await g.init()
    await expect(
      g.rpc.call(METHODS.openLink, { url: "javascript:alert(1)" })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
    await expect(
      g.rpc.call(METHODS.openLink, { url: "nope" })
    ).rejects.toMatchObject({
      code: ERROR.invalidParams,
    })
    await expect(g.rpc.call("tools/list")).rejects.toMatchObject({
      code: ERROR.methodNotFound,
    })
    host.teardown()
  })

  it("tears down after three unanswered pings", async () => {
    vi.useFakeTimers({
      toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout"],
    })
    const onLost = vi.fn()
    const { host, guest: g } = mount({
      answerPings: false,
      onLost,
      pingMs: 1000,
    })
    await g.init()
    for (let i = 0; i <= PING_MISSES; i++) {
      vi.advanceTimersByTime(1000)
      await settle()
    }
    expect(onLost).toHaveBeenCalledTimes(1)
    expect(host.closed).toBe(true)
  })

  it("keeps a guest that answers", async () => {
    vi.useFakeTimers({
      toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout"],
    })
    const onLost = vi.fn()
    const { host, guest: g } = mount({ onLost, pingMs: 1000 })
    await g.init()
    for (let i = 0; i < 10; i++) {
      vi.advanceTimersByTime(1000)
      await settle()
    }
    expect(onLost).not.toHaveBeenCalled()
    expect(host.closed).toBe(false)
    host.teardown()
    expect(host.closed).toBe(true)
  })
})
