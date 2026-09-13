/** The bridge on the console side: the JSON-RPC framing both ends share, the
 * grant every call goes through by resolved identity, and the host's own
 * behaviour over a port: a subscription answered with its page and pushed
 * again when the cache moves, a fan-out rewritten per kind, a function gated
 * by the person's confirm, agent events relayed per stream, the chrome
 * relayed to callbacks, every call refused below owner provenance, and a
 * guest that stops answering pings torn down. */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { AgentEvent } from "@/lib/api/agents"
import type { ListParams } from "@/lib/api/records"
import {
  ApiError,
  type KindInfo,
  type Page,
  type SubstrateRecord,
} from "@/lib/api/types"
import { expandGrant, type Grant } from "../grant"
import { createSubscriptions } from "../subscriptions"
import {
  checkAccess,
  createBridgeHost,
  ownerProvenance,
  PING_MISSES,
  type AgentsApi,
  type AppGrant,
  type FunctionsApi,
  type HostCallbacks,
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
  type GuestError,
  type HostContext,
  type InitializeResult,
  type JsonRpcNotification,
  type RecordsPush,
  type SubscribeResult,
} from "./protocol"

const TASK = "ada.example.com/tasks/task"
const EVENT = "ada.example.com/calendar/calendarevent"
const PERSON = "ada.example.com/people/person"
const TEMPORAL = "substrate.reamde.dev/core/temporal"
const SUMMARIZE = "ada.example.com/tasks/summarize"
const PLANNER = "ada.example.com/tasks/planner"

const taskKind: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    traits: ["temporal(point: dueAt)"],
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

const eventKind: KindInfo = {
  ...taskKind,
  identity: EVENT,
  name: "calendarevent",
  package: "calendar",
  definition: { traits: ["temporal(range)"], properties: {} },
}

const personKind: KindInfo = {
  ...taskKind,
  identity: PERSON,
  name: "person",
  package: "people",
  definition: { properties: {} },
}
const kinds = [taskKind, eventKind, personKind]

function spec(over: Partial<Grant> = {}): AppGrant {
  return {
    id: "tasks",
    permissions: {
      reads: { kinds: [TASK], traits: [] },
      writes: [],
      call: [],
      agents: [],
      ...over,
    },
  }
}

const grantOf = (s: AppGrant) => expandGrant(s, kinds)

function record(
  id: string,
  status = "todo",
  kind = TASK,
  extra: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    id,
    kind,
    properties: { title: id, status, ...extra },
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
  app: { id: "tasks", name: "Tasks", route: "", inputs: {} },
  kinds: [taskKind],
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

const tick = (ms = 20) => new Promise((r) => setTimeout(r, ms))

function fakeApi(
  pages?: (p: ListParams) => Page
): RecordsApi & { calls: string[] } {
  const calls: string[] = []
  return {
    calls,
    list: async (p) => {
      calls.push(
        `list ${p.name} ${p.orderBy ?? ""} ${JSON.stringify(p.filter ?? {})}`.trim()
      )
      return pages ? pages(p) : page
    },
    get: async (_k, id) => {
      calls.push(`get ${id}`)
      return record(id, "doing")
    },
    create: async (_k, input, key) => {
      calls.push(`create ${key ? "keyed" : "bare"}`)
      return record("new", "todo", TASK, input.properties)
    },
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
function guest(
  port: MessagePort,
  opts: { answerPings?: boolean; handlesBack?: boolean } = {}
): Guest {
  const notices: JsonRpcNotification[] = []
  const rpc = new Endpoint(port, {
    onRequest(method) {
      if (method === METHODS.ping) {
        if (opts.answerPings === false) return new Promise(() => {})
        return {}
      }
      if (method === METHODS.back) return { handled: opts.handlesBack === true }
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

function mount(opts: {
  spec?: AppGrant
  granted?: boolean
  api?: RecordsApi
  functions?: FunctionsApi
  agents?: AgentsApi
  queryClient?: QueryClient
  answerPings?: boolean
  handlesBack?: boolean
  pingMs?: number
  callbacks?: Partial<HostCallbacks>
}) {
  const [hostPort, guestPort] = pair()
  const app = opts.spec ?? spec()
  const host = createBridgeHost({
    app: () => ({ spec: app, kinds }),
    granted: () => opts.granted ?? true,
    api: opts.api ?? fakeApi(),
    functions: opts.functions,
    agents: opts.agents,
    queryClient: opts.queryClient,
    pingMs: opts.pingMs,
    callbacks: { hostContext: () => hostContext, ...opts.callbacks },
  })
  host.attach(hostPort)
  return {
    host,
    guest: guest(guestPort, {
      answerPings: opts.answerPings,
      handlesBack: opts.handlesBack,
    }),
  }
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
    const access = checkAccess(grantOf(spec()), kinds, "read", TASK)
    expect(access.ok).toBe(true)
    if (access.ok) expect(access.kind.identity).toBe(TASK)
  })

  it("refuses a read of an undeclared kind and names the fix", () => {
    expect(checkAccess(grantOf(spec()), kinds, "read", PERSON)).toEqual({
      ok: false,
      message: `the app's grant does not read ${PERSON}; permissions.reads.kinds: add ${PERSON}`,
      problem: { path: "permissions.reads.kinds", message: `add ${PERSON}` },
    })
  })

  it("refuses a kind the registry does not know, and a call naming none", () => {
    const g = grantOf(spec())
    expect(checkAccess(g, kinds, "read", "ada.example.com/tasks/Task").ok).toBe(
      false
    )
    expect(checkAccess(g, kinds, "read", `${TASK}/`).ok).toBe(false)
    expect(checkAccess(g, kinds, "read", undefined).ok).toBe(false)
    expect(checkAccess(g, kinds, "read", 7).ok).toBe(false)
  })

  it("refuses a write without a writes grant, and passes one with it", () => {
    expect(checkAccess(grantOf(spec()), kinds, "write", TASK).ok).toBe(false)
    expect(
      checkAccess(grantOf(spec({ writes: [TASK] })), kinds, "write", TASK).ok
    ).toBe(true)
    expect(
      checkAccess(grantOf(spec({ writes: [TASK] })), kinds, "write", PERSON).ok
    ).toBe(false)
  })

  it("reads a trait's implementors", () => {
    const g = grantOf(spec({ reads: { kinds: [], traits: [TEMPORAL] } }))
    expect(g.reads).toEqual([TASK, EVENT])
    expect(checkAccess(g, kinds, "read", EVENT).ok).toBe(true)
    expect(checkAccess(g, kinds, "read", PERSON).ok).toBe(false)
  })
})

describe("owner provenance", () => {
  const row = (
    meta: Record<string, { tier?: string }>,
    props?: Record<string, unknown>
  ): SubstrateRecord => ({
    ...record("a"),
    properties: props ?? {
      source: "export default () => null",
      modules: { helper: "export const x = 1" },
      permissions: {},
    },
    propertyMeta: meta as SubstrateRecord["propertyMeta"],
  })
  const owner = { tier: "owner" }

  it("holds when every gated property present is the owner's", () => {
    expect(
      ownerProvenance(
        row({ source: owner, modules: owner, permissions: owner })
      )
    ).toBe(true)
    // A row without modules has no modules provenance to ask for.
    expect(
      ownerProvenance(
        row(
          { source: owner, permissions: owner },
          { source: "x", permissions: {} }
        )
      )
    ).toBe(true)
    // `name` and `icon` are not gated.
    expect(
      ownerProvenance(
        row(
          { source: owner, permissions: owner, name: { tier: "machine" } },
          { source: "x", permissions: {}, name: "Tasks" }
        )
      )
    ).toBe(true)
  })

  it("fails when one is below owner, unrecorded, or the source is missing", () => {
    expect(
      ownerProvenance(
        row({ source: owner, modules: owner, permissions: { tier: "machine" } })
      )
    ).toBe(false)
    expect(ownerProvenance(row({ source: owner, permissions: owner }))).toBe(
      false
    )
    expect(ownerProvenance(row({}))).toBe(false)
    expect(ownerProvenance(row({ source: owner }, { permissions: {} }))).toBe(
      false
    )
    expect(ownerProvenance(undefined)).toBe(false)
  })
})

describe("the host over a port", () => {
  it("answers initialize with the host context, app and kinds included", async () => {
    const { host, guest: g } = mount({})
    const init = await g.init()
    expect(init.hostContext).toEqual(hostContext)
    expect(init.hostContext.app.id).toBe("tasks")
    expect(init.protocolVersion).toBe("2026-01-26")
    host.teardown()
  })

  it("serves a list of a declared kind and refuses one of an undeclared kind, naming the fix", async () => {
    const api = fakeApi()
    const errors: GuestError[] = []
    const { host, guest: g } = mount({
      api,
      callbacks: { onError: (e) => errors.push(e) },
    })
    await g.init()
    const result = await g.call(TOOLS.list, { kind: TASK, first: 10 })
    expect(result.structuredContent).toEqual({
      records: page.records,
      cursor: undefined,
      head: 42,
    })
    await expect(g.call(TOOLS.list, { kind: PERSON })).rejects.toMatchObject({
      code: ERROR.forbidden,
      message: `the app's grant does not read ${PERSON}; permissions.reads.kinds: add ${PERSON}`,
    })
    expect(errors).toEqual([
      {
        phase: "grant",
        path: "permissions.reads.kinds",
        message: `add ${PERSON}`,
      },
    ])
    expect(api.calls).toEqual(["list task  {}"])
    host.teardown()
  })

  it("fans an implements query out per kind, at rewritten to each point, and says incomplete", async () => {
    const api = fakeApi((p) =>
      p.name === "task"
        ? {
            records: [
              record("t", "todo", TASK, { dueAt: "2026-09-05T00:00:00Z" }),
            ],
            cursor: "more",
            head: 9,
            generation: "g",
          }
        : {
            records: [record("e", "x", EVENT, { at: "2026-09-04T00:00:00Z" })],
            head: 9,
            generation: "g",
          }
    )
    const { host, guest: g } = mount({
      api,
      spec: spec({ reads: { kinds: [], traits: [TEMPORAL] } }),
    })
    await g.init()
    const result = await g.call(TOOLS.list, {
      implements: TEMPORAL,
      filter: { properties: { at: { gte: "2026-09-01T00:00:00Z" } } },
      orderBy: "at:asc",
      first: 50,
    })
    const content = result.structuredContent as {
      records: SubstrateRecord[]
      incomplete?: boolean
      cursor?: string
    }
    expect(content.records.map((r) => r.id)).toEqual(["e", "t"])
    expect(content.incomplete).toBe(true)
    expect(content.cursor).toBeUndefined()
    expect(api.calls).toEqual([
      'list task dueAt:asc {"properties":{"dueAt":{"gte":"2026-09-01T00:00:00Z"}}}',
      'list calendarevent at:asc {"properties":{"at":{"gte":"2026-09-01T00:00:00Z"}}}',
    ])
    // A trait outside the grant is refused.
    await expect(
      g.call(TOOLS.list, { implements: "substrate.reamde.dev/core/nothing" })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
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
    // A create without a key is keyed by the host, so a retry lands once.
    await granted.guest.call(TOOLS.put, {
      kind: TASK,
      properties: { title: "n" },
    })
    expect(api.calls.at(-1)).toBe("create keyed")
    granted.host.teardown()
  })

  it("carries the API's envelope through a failed tool call", async () => {
    const api = fakeApi()
    api.patch = async () => {
      throw new ApiError(
        "validation",
        "title is required",
        422,
        [],
        [{ path: "data.properties.title", message: "required" }]
      )
    }
    const { host, guest: g } = mount({ api, spec: spec({ writes: [TASK] }) })
    await g.init()
    const result = await g.call(TOOLS.patch, {
      kind: TASK,
      id: "t1",
      properties: { title: "" },
    })
    expect(result.isError).toBe(true)
    expect(result.structuredContent).toEqual({
      code: "validation",
      message: "title is required",
      problems: undefined,
      problemDetails: [{ path: "data.properties.title", message: "required" }],
    })
    host.teardown()
  })

  it("below owner provenance refuses every call, subscription and confirm, whatever the row grants", async () => {
    const api = fakeApi()
    const confirm = vi.fn(async () => true)
    const { host, guest: g } = mount({
      api,
      granted: false,
      spec: spec({ writes: [TASK], call: [SUMMARIZE] }),
      queryClient: new QueryClient(),
      callbacks: { confirm },
    })
    await g.init()
    for (const [tool, args] of [
      [TOOLS.list, { kind: TASK }],
      [TOOLS.get, { kind: TASK, id: "t1" }],
      [TOOLS.put, { kind: TASK, properties: {} }],
      [TOOLS.functionCall, { ref: SUMMARIZE, args: {} }],
    ] as const) {
      await expect(g.call(tool, { ...args })).rejects.toMatchObject({
        code: ERROR.forbidden,
      })
    }
    await expect(
      g.rpc.call(METHODS.subscribe, { kind: TASK })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
    await expect(
      g.rpc.call(METHODS.confirm, { title: "Sure?" })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
    expect(api.calls).toEqual([])
    expect(confirm).not.toHaveBeenCalled()
    host.teardown()
  })

  it("answers a subscription with its page, pushes when the cache moves, and closes it", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    let current = page
    const api = fakeApi(() => current)
    const subscribed: string[][] = []
    const [hostPort, guestPort] = pair()
    const host = createBridgeHost({
      app: () => ({ spec: spec(), kinds }),
      granted: () => true,
      api,
      subscriptions: createSubscriptions(client, (p) => ({
        queryKey: ["records", p.authority, p.package, p.name, { f: p.filter }],
        queryFn: () => api.list(p),
      })),
      callbacks: {
        hostContext: () => hostContext,
        onSubscribedKinds: (k) => subscribed.push(k),
      },
    })
    host.attach(hostPort)
    const g = guest(guestPort)
    await g.init()

    const res = await g.rpc.call<SubscribeResult>(METHODS.subscribe, {
      kind: TASK,
      orderBy: "dueAt:asc",
    })
    expect(res.subscription).toBe("sub-1")
    expect(res.page.records.map((r) => r.id)).toEqual(["t1", "t2"])
    expect(subscribed).toEqual([[TASK]])
    expect(host.subscribedKinds()).toEqual([TASK])

    current = { ...page, records: [record("t3")], head: 43 }
    await client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    const pushes = g.notices.filter((n) => n.method === METHODS.records)
    expect(pushes).toHaveLength(1)
    expect(
      (pushes[0].params as RecordsPush).page.records.map((r) => r.id)
    ).toEqual(["t3"])

    await expect(
      g.rpc.call(METHODS.subscribe, { kind: PERSON })
    ).rejects.toMatchObject({ code: ERROR.forbidden })

    await g.rpc.call(METHODS.unsubscribe, { subscription: "sub-1" })
    expect(host.subscribedKinds()).toEqual([])
    current = { ...page, records: [], head: 44 }
    await client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    expect(g.notices.filter((n) => n.method === METHODS.records)).toHaveLength(
      1
    )
    host.teardown()
  })

  it("re-checks a subscription's grant on every push", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    let current = page
    let app = spec()
    const api = fakeApi(() => current)
    const [hostPort, guestPort] = pair()
    const host = createBridgeHost({
      app: () => ({ spec: app, kinds }),
      granted: () => true,
      api,
      subscriptions: createSubscriptions(client, (p) => ({
        queryKey: ["records", p.name],
        queryFn: () => api.list(p),
      })),
      callbacks: { hostContext: () => hostContext },
    })
    host.attach(hostPort)
    const g = guest(guestPort)
    await g.init()
    await g.rpc.call<SubscribeResult>(METHODS.subscribe, { kind: TASK })
    app = spec({ reads: { kinds: [PERSON], traits: [] } })
    current = { ...page, records: [record("t9")] }
    await client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    const push = g.notices.find((n) => n.method === METHODS.records)
    expect((push?.params as RecordsPush).page).toMatchObject({
      records: [],
      error: { code: "forbidden" },
    })
    host.teardown()
  })

  it("re-reads every open subscription when the grant or the registry moves", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    // The calendar package is not installed yet.
    let registry = [taskKind, personKind]
    let app = spec({ reads: { kinds: [], traits: [TEMPORAL] } })
    const api = fakeApi((p) =>
      p.name === "task"
        ? page
        : { records: [record("e1", "open", EVENT)], head: 7, generation: "g" }
    )
    const errors: GuestError[] = []
    const subscribed: string[][] = []
    const [hostPort, guestPort] = pair()
    const host = createBridgeHost({
      app: () => ({ spec: app, kinds: registry }),
      granted: () => true,
      api,
      subscriptions: createSubscriptions(client, (p) => ({
        queryKey: ["records", p.name],
        queryFn: () => api.list(p),
      })),
      callbacks: {
        hostContext: () => hostContext,
        onError: (e) => errors.push(e),
        onSubscribedKinds: (k) => subscribed.push(k),
      },
    })
    host.attach(hostPort)
    const g = guest(guestPort)
    await g.init()
    const pages = () =>
      g.notices
        .filter((n) => n.method === METHODS.records)
        .map((n) => (n.params as RecordsPush).page)
    const ids = (p: RecordsPush["page"] | undefined) =>
      (p?.records ?? []).map((r) => r.id).sort()

    const res = await g.rpc.call<SubscribeResult>(METHODS.subscribe, {
      implements: TEMPORAL,
    })
    expect(ids(res.page)).toEqual(["t1", "t2"])
    expect(host.subscribedKinds()).toEqual([TASK])

    // The calendar package lands: the trait read acquires its implementor
    // and the guest hears the wider page now, with no data change needed.
    registry = [taskKind, eventKind, personKind]
    host.grantChanged()
    await tick()
    expect(ids(pages().at(-1))).toEqual(["e1", "t1", "t2"])
    expect(host.subscribedKinds()).toEqual([EVENT, TASK])
    expect(subscribed.at(-1)).toEqual([EVENT, TASK])

    // A move that changes no read pushes nothing.
    const quiet = pages().length
    host.grantChanged()
    await tick()
    expect(pages().length).toBe(quiet)

    // The grant narrows: the refusal is pushed now, the strip told once, and
    // no observer is held.
    app = spec({ reads: { kinds: [PERSON], traits: [] } })
    host.grantChanged()
    await tick()
    expect(pages().at(-1)).toMatchObject({
      records: [],
      error: { code: "forbidden" },
    })
    expect(errors).toHaveLength(1)
    expect(errors[0]).toMatchObject({
      phase: "grant",
      path: "permissions.reads.traits",
    })
    expect(host.subscribedKinds()).toEqual([])
    const refused = pages().length
    host.grantChanged()
    await tick()
    expect(pages().length).toBe(refused)
    expect(errors).toHaveLength(1)

    // Widened again: the subscription reopens and the page flows.
    app = spec({ reads: { kinds: [], traits: [TEMPORAL] } })
    host.grantChanged()
    await tick()
    expect(ids(pages().at(-1))).toEqual(["e1", "t1", "t2"])
    expect(host.subscribedKinds()).toEqual([EVENT, TASK])
    host.teardown()
  })

  it("answers a subscribe whose first page had not settled when its reads were reopened", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    let registry = [taskKind, personKind]
    const app = spec({ reads: { kinds: [], traits: [TEMPORAL] } })
    // The task page is held back until the registry has moved under the
    // subscribe: the observer it opened is replaced before it ever settles.
    let release!: () => void
    const held = new Promise<void>((r) => {
      release = r
    })
    const api = fakeApi()
    const slow: RecordsApi = {
      ...api,
      list: async (p) => {
        if (p.name === "task") {
          await held
          return page
        }
        return {
          records: [record("e1", "open", EVENT)],
          head: 7,
          generation: "g",
        }
      },
    }
    const [hostPort, guestPort] = pair()
    const host = createBridgeHost({
      app: () => ({ spec: app, kinds: registry }),
      granted: () => true,
      api: slow,
      subscriptions: createSubscriptions(client, (p) => ({
        queryKey: ["records", p.name],
        queryFn: () => slow.list(p),
      })),
      callbacks: { hostContext: () => hostContext },
    })
    host.attach(hostPort)
    const g = guest(guestPort)
    await g.init()

    const answer = g.rpc.call<SubscribeResult>(METHODS.subscribe, {
      implements: TEMPORAL,
    })
    await tick()
    registry = [taskKind, eventKind, personKind]
    host.grantChanged()
    await tick()
    release()
    const res = await answer
    expect((res.page.records ?? []).map((r) => r.id).sort()).toEqual([
      "e1",
      "t1",
      "t2",
    ])
    // It arrived as the answer, the only way the guest could take it: no
    // push went out for a subscription the guest did not yet know.
    expect(g.notices.filter((n) => n.method === METHODS.records)).toHaveLength(
      0
    )
  })

  it("delivers a read the widened set cannot order as the subscription's page, and throws nothing", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    const scored: KindInfo = {
      ...taskKind,
      definition: {
        ...taskKind.definition,
        properties: {
          ...(taskKind.definition.properties ?? {}),
          priority: { type: "int" },
        },
      },
    }
    const named: KindInfo = {
      ...eventKind,
      definition: {
        traits: ["temporal(range)"],
        properties: { priority: { type: "string" } },
      },
    }
    let registry = [scored, personKind]
    const app = spec({ reads: { kinds: [], traits: [TEMPORAL] } })
    const api = fakeApi((p) =>
      p.name === "task"
        ? page
        : { records: [record("e1", "open", EVENT)], head: 7, generation: "g" }
    )
    const [hostPort, guestPort] = pair()
    const host = createBridgeHost({
      app: () => ({ spec: app, kinds: registry }),
      granted: () => true,
      api,
      subscriptions: createSubscriptions(client, (p) => ({
        queryKey: ["records", p.name],
        queryFn: () => api.list(p),
      })),
      callbacks: { hostContext: () => hostContext },
    })
    host.attach(hostPort)
    const g = guest(guestPort)
    await g.init()
    const pages = () =>
      g.notices
        .filter((n) => n.method === METHODS.records)
        .map((n) => (n.params as RecordsPush).page)

    // One implementor: the order is the server's to judge, and it answers.
    const res = await g.rpc.call<SubscribeResult>(METHODS.subscribe, {
      implements: TEMPORAL,
      orderBy: "priority:asc",
    })
    expect(res.page.records?.map((r) => r.id)).toEqual(["t1", "t2"])

    // A second implementor orders `priority` as text: the fan-out is refused
    // where the subscribe would have refused it, as this subscription's own
    // page, and the reconcile that met it returns normally.
    registry = [scored, named, personKind]
    expect(() => host.grantChanged()).not.toThrow()
    await tick()
    expect(pages().at(-1)).toMatchObject({
      records: [],
      error: {
        code: "bad_request",
        message: expect.stringContaining("priority"),
      },
    })
    expect(host.subscribedKinds()).toEqual([])
    const quiet = pages().length
    host.grantChanged()
    await tick()
    expect(pages().length).toBe(quiet)
  })

  it("gates a function on the person's confirm by its effect and confirmation", async () => {
    const confirm = vi.fn(async () => true)
    const called: string[] = []
    const functions: FunctionsApi = {
      get: async (id) => ({
        ...record(id),
        properties: {
          name: "Summarize",
          effect: id === SUMMARIZE ? "external" : "read",
          confirmation: id === PLANNER ? "always" : undefined,
        },
      }),
      call: async (id, input, key) => {
        called.push(`${id} ${JSON.stringify(input)} ${key ? "keyed" : "bare"}`)
        return { output: { ok: true }, effects: 1 }
      },
    }
    const { host, guest: g } = mount({
      functions,
      spec: spec({ call: [SUMMARIZE, PLANNER, "ada.example.com/tasks/read"] }),
      callbacks: { confirm },
    })
    await g.init()
    const result = await g.call(TOOLS.functionCall, {
      ref: SUMMARIZE,
      args: { id: "t1" },
    })
    expect(result.structuredContent).toEqual({
      output: { ok: true },
      effects: 1,
    })
    expect(confirm).toHaveBeenCalledWith({
      title: "Run Summarize?",
      body: undefined,
      destructive: false,
    })
    await g.call(TOOLS.functionCall, { ref: PLANNER, args: {} })
    expect(confirm).toHaveBeenCalledTimes(2)
    await g.call(TOOLS.functionCall, {
      ref: "ada.example.com/tasks/read",
      args: {},
    })
    expect(confirm).toHaveBeenCalledTimes(2)
    expect(called.every((c) => c.endsWith("keyed"))).toBe(true)

    confirm.mockResolvedValueOnce(false)
    await expect(
      g.call(TOOLS.functionCall, { ref: SUMMARIZE, args: {} })
    ).rejects.toMatchObject({
      code: ERROR.forbidden,
      message: "the person declined",
    })
    await expect(
      g.call(TOOLS.functionCall, {
        ref: "ada.example.com/tasks/other",
        args: {},
      })
    ).rejects.toMatchObject({
      code: ERROR.forbidden,
      message: `the app's grant does not call ada.example.com/tasks/other; permissions.call: add ada.example.com/tasks/other`,
    })
    expect(called).toHaveLength(3)
    host.teardown()
  })

  it("relays agent events per stream and stops one on request", async () => {
    const handlers: Record<string, (e: AgentEvent) => void> = {}
    const stopped: string[] = []
    const agents: AgentsApi = {
      chat: (opts) => {
        handlers[opts.message] = opts.onEvent
        return { stop: () => stopped.push(opts.message) }
      },
    }
    const { host, guest: g } = mount({
      agents,
      spec: spec({ agents: [PLANNER] }),
    })
    await g.init()
    const a = await g.call(TOOLS.agentChat, { ref: PLANNER, message: "plan" })
    const b = await g.call(TOOLS.agentChat, { ref: PLANNER, message: "again" })
    const streamA = (a.structuredContent as { stream: string }).stream
    const streamB = (b.structuredContent as { stream: string }).stream
    expect(streamA).not.toBe(streamB)
    handlers.plan({ kind: "delta", text: "hi" })
    handlers.again({ kind: "thread", thread: "th" })
    await settle()
    const events = g.notices
      .filter((n) => n.method === METHODS.agentEvent)
      .map((n) => n.params as { stream: string; event: AgentEvent })
    expect(events).toEqual([
      { stream: streamA, event: { kind: "delta", text: "hi" } },
      { stream: streamB, event: { kind: "thread", thread: "th" } },
    ])
    await g.call(TOOLS.agentStop, { stream: streamA })
    expect(stopped).toEqual(["plan"])
    await expect(
      g.call(TOOLS.agentChat, {
        ref: "ada.example.com/tasks/other",
        message: "x",
      })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
    host.teardown()
    expect(stopped).toEqual(["plan", "again"])
  })

  it("relays the chrome: title, toast, primary action, confirm, navigate, route, errors", async () => {
    const seen: string[] = []
    const { host, guest: g } = mount({
      callbacks: {
        onTitle: (t) => seen.push(`title ${t}`),
        onToast: (t) => seen.push(`toast ${t.title} ${t.type ?? "-"}`),
        onPrimaryAction: (p) => seen.push(`primary ${p ? p.label : "none"}`),
        confirm: async (a) => {
          seen.push(`confirm ${a.title} ${a.destructive ? "!" : ""}`)
          return true
        },
        onNavigate: (t) => {
          seen.push(`navigate ${JSON.stringify(t)}`)
        },
        onError: (e) => seen.push(`error ${e.phase} ${e.module}:${e.line}`),
      },
    })
    await g.init()
    g.rpc.notify(METHODS.title, { text: "Tasks" })
    g.rpc.notify(METHODS.toast, { title: "Saved", type: "success" })
    g.rpc.notify(METHODS.primaryAction, { label: "Add" })
    g.rpc.notify(METHODS.primaryAction, null)
    g.rpc.notify(METHODS.error, {
      phase: "transform",
      message: "Unexpected token",
      module: "source",
      line: 14,
      column: 8,
    })
    await expect(
      g.rpc.call(METHODS.confirm, { title: "Delete?", destructive: true })
    ).resolves.toEqual({ ok: true })
    await g.rpc.call(METHODS.navigate, { path: "/website" })
    await g.rpc.call(METHODS.navigate, { record: { kind: TASK, id: "t1" } })
    await g.rpc.call(METHODS.navigate, { back: true })
    await expect(g.rpc.call(METHODS.navigate, {})).rejects.toMatchObject({
      code: ERROR.invalidParams,
    })
    await settle()
    expect(seen).toEqual([
      "title Tasks",
      "toast Saved success",
      "primary Add",
      "primary none",
      "error transform source:14",
      "confirm Delete? !",
      'navigate {"path":"/website"}',
      `navigate {"record":{"kind":"${TASK}","id":"t1"}}`,
      'navigate {"back":true}',
    ])
    host.routeChanged("/website")
    host.primaryActionClicked()
    await settle()
    expect(g.notices.filter((n) => n.method === METHODS.routeChanged)).toEqual([
      notification(METHODS.routeChanged, { path: "/website" }),
    ])
    expect(
      g.notices.some((n) => n.method === METHODS.primaryActionClicked)
    ).toBe(true)
    host.teardown()
  })

  it("asks the guest about a back tap and reports whether it was handled", async () => {
    const quiet = mount({})
    await quiet.guest.init()
    await expect(quiet.host.back()).resolves.toBe(false)
    quiet.host.teardown()
    const handles = mount({ handlesBack: true })
    await handles.guest.init()
    await expect(handles.host.back()).resolves.toBe(true)
    handles.host.teardown()
  })

  it("refuses a link off http, https, mailto and tel, and unknown methods", async () => {
    const opened: string[] = []
    const { host, guest: g } = mount({
      callbacks: {
        confirmLink: () => true,
        openLink: (u) => opened.push(u.href),
      },
    })
    await g.init()
    await expect(
      g.rpc.call(METHODS.openLink, { url: "javascript:alert(1)" })
    ).rejects.toMatchObject({ code: ERROR.forbidden })
    await expect(
      g.rpc.call(METHODS.openLink, { url: "nope" })
    ).rejects.toMatchObject({ code: ERROR.invalidParams })
    await g.rpc.call(METHODS.openLink, { url: "tel:+15551234" })
    await g.rpc.call(METHODS.openLink, { url: "mailto:ada@example.com" })
    expect(opened).toEqual(["tel:+15551234", "mailto:ada@example.com"])
    await expect(g.rpc.call("tools/list")).rejects.toMatchObject({
      code: ERROR.methodNotFound,
    })
    host.teardown()
  })

  it("answers a declined link with opened: false instead of an error", async () => {
    const opened: string[] = []
    const { host, guest: g } = mount({
      callbacks: {
        confirmLink: () => false,
        openLink: (u) => opened.push(u.href),
      },
    })
    await g.init()
    await expect(
      g.rpc.call(METHODS.openLink, { url: "mailto:ada@example.com" })
    ).resolves.toEqual({ opened: false })
    expect(opened).toEqual([])
    host.teardown()
  })

  it("tears down after three unanswered pings", async () => {
    vi.useFakeTimers({
      toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout"],
    })
    const onLost = vi.fn()
    const { host, guest: g } = mount({
      answerPings: false,
      pingMs: 1000,
      callbacks: { onLost },
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
    const { host, guest: g } = mount({ pingMs: 1000, callbacks: { onLost } })
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
