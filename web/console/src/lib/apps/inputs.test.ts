// @vitest-environment jsdom
/** The three-step input rule and its refusals: bound, then `default`, then
 * the sole record, each read BY ID; a binding that names nothing the kind
 * holds is `missing` and never silently replaced; a failed read is `error`;
 * and an input whose kind the grant does not read is `ungranted`, never
 * queried and never handed to the guest, whatever the row's `inputs` say. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { renderHook, waitFor } from "@testing-library/react"
import { createElement, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { appSpec } from "./app-spec"
import { expandGrant } from "./grant"
import {
  grantedInputs,
  inputStatus,
  resolveInput,
  useAppInputs,
  type Lookup,
} from "./inputs"

const USER = "providers.example.com/github/user"
const TASK = "ada.example.com/tasks/task"

function kindInfo(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { properties: {} },
  }
}
const userKind = kindInfo(USER)
const taskKind = kindInfo(TASK)
const kinds = [userKind, taskKind]

const account = (id: string, kind = USER): SubstrateRecord => ({
  id,
  kind,
  properties: {},
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const ok = (...records: SubstrateRecord[]): Lookup => ({
  status: "ok",
  records,
})
const pending: Lookup = { status: "pending" }
const failed: Lookup = { status: "error", message: "boom" }

describe("resolveInput", () => {
  it("takes the binding first, read by its own id", () => {
    const state = resolveInput(
      `${USER}/work`,
      USER,
      ok(account("work")),
      pending
    )
    expect(state).toMatchObject({ state: "bound", record: { id: "work" } })
  })

  it("then the record called default, then the sole record", () => {
    expect(
      resolveInput(undefined, USER, ok(account("default")), pending)
    ).toMatchObject({ state: "default" })
    expect(
      resolveInput(undefined, USER, ok(), ok(account("only")))
    ).toMatchObject({ state: "sole", record: { id: "only" } })
  })

  it("is ambiguous with two, none with zero, and loading until both reads answer", () => {
    expect(
      resolveInput(undefined, USER, ok(), ok(account("a"), account("b")))
    ).toEqual({ state: "ambiguous" })
    expect(resolveInput(undefined, USER, ok(), ok())).toEqual({ state: "none" })
    expect(resolveInput(undefined, USER, pending, ok())).toEqual({
      state: "loading",
    })
    expect(resolveInput(undefined, USER, ok(), pending)).toEqual({
      state: "loading",
    })
  })

  it("keeps a binding that no longer resolves as missing, never a fallback", () => {
    const gone = resolveInput(
      `${USER}/gone`,
      USER,
      ok(),
      ok(account("default"))
    )
    expect(gone).toEqual({ state: "missing", binding: `${USER}/gone` })
    // Another kind's record is missing before any read is made.
    const wrongKind = resolveInput(`${TASK}/default`, USER, pending, pending)
    expect(wrongKind.state).toBe("missing")
    expect(inputStatus("me", gone)).toMatch(/no longer exists/)
  })

  it("is an error, not a load that never ends, when a read fails", () => {
    expect(resolveInput(`${USER}/work`, USER, failed, pending)).toEqual({
      state: "error",
      message: "boom",
    })
    expect(resolveInput(undefined, USER, ok(), failed)).toEqual({
      state: "error",
      message: "boom",
    })
    expect(inputStatus("me", { state: "error", message: "boom" })).toMatch(
      /could not be read: boom/
    )
  })
})

describe("grantedInputs", () => {
  it("hands the guest only the inputs whose kind the grant reads", () => {
    const inputs = { me: { kind: USER }, other: { kind: TASK } }
    const states = {
      me: { state: "sole" as const, record: account("only") },
      other: { state: "sole" as const, record: account("t", TASK) },
      stray: { state: "ungranted" as const, kind: TASK },
    }
    expect(Object.keys(grantedInputs(states, inputs, [USER], true))).toEqual([
      "me",
    ])
    expect(grantedInputs(states, inputs, [USER, TASK], false)).toEqual({})
    expect(inputStatus("other", states.stray)).toMatch(
      /outside the app's grant/
    )
  })
})

describe("useAppInputs", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  /** The user collection as the wire would serve it: `default` and the
   * bound record answer an `ids` filter, the probe answers a bare `first=2`,
   * and a `status` other than 200 fails every read. */
  function serve(users: SubstrateRecord[], status = 200) {
    fetchMock.mockImplementation(async (url) => {
      const u = new URL(String(url), "http://console")
      if (status !== 200) {
        return new Response(JSON.stringify({ message: "down" }), { status })
      }
      const filter = u.searchParams.get("filter")
      const first = Number(u.searchParams.get("first") ?? 50)
      const ids = filter
        ? ((JSON.parse(filter) as { ids?: string[] }).ids ?? [])
        : undefined
      const records = (
        ids ? users.filter((r) => ids.includes(r.id)) : users
      ).slice(0, first)
      return new Response(
        JSON.stringify({ records, head: 1, generation: "g" }),
        { status: 200 }
      )
    })
  }

  function app(
    over: Record<string, unknown> = {},
    reads: string[] = [USER]
  ): SubstrateRecord {
    return {
      id: "pulls",
      kind: "substrate.reamde.dev/core/app",
      properties: {
        name: "Pulls",
        runtime: "react",
        source: "export default () => null",
        permissions: {
          reads: {
            kinds: reads.map((k) => ({
              ref: `substrate.reamde.dev/core/kind/${k}`,
            })),
          },
        },
        inputs: {
          me: { kind: { ref: `substrate.reamde.dev/core/kind/${USER}` } },
        },
        ...over,
      },
      labels: {},
      version: 1,
      createdAt: "",
      updatedAt: "",
    }
  }

  function resolve(record: SubstrateRecord, granted = true) {
    const spec = appSpec(record, kinds)
    const reads = expandGrant(spec, kinds).reads
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children)
    return renderHook(() => useAppInputs(spec, kinds, { granted, reads }), {
      wrapper,
    })
  }

  const asked = () => fetchMock.mock.calls.map((c) => String(c[0]))

  it("never reads an input whose kind the grant does not cover: owner-written code and grant, machine-written input", async () => {
    serve([account("default")])
    // The grant reads tasks; the input (which an agent may write) names the
    // user kind. The record exists, and the guest must not get it.
    const { result } = resolve(app({}, [TASK]))
    await new Promise((r) => setTimeout(r, 30))
    expect(result.current.states.me).toEqual({ state: "ungranted", kind: USER })
    expect(result.current.records.me).toBeUndefined()
    expect(result.current.kinds).toEqual([])
    expect(asked()).toEqual([])
    // The same below owner provenance, whatever the grant says.
    const below = resolve(app(), false)
    await new Promise((r) => setTimeout(r, 30))
    expect(below.result.current.states.me.state).toBe("ungranted")
    expect(asked()).toEqual([])
  })

  it("reads a bound record by its id, one read, whatever the collection's size", async () => {
    serve([account("default"), account("work")])
    const { result } = resolve(
      app({ bindings: { me: { ref: `${USER}/work` } } })
    )
    await waitFor(() =>
      expect(result.current.states.me).toMatchObject({
        state: "bound",
        record: { id: "work" },
      })
    )
    expect(asked()).toHaveLength(1)
    const u = new URL(asked()[0], "http://console")
    expect(u.pathname).toBe(`/api/v1/${USER}`)
    expect(u.searchParams.get("first")).toBe("1")
    expect(JSON.parse(u.searchParams.get("filter")!)).toEqual({ ids: ["work"] })
    expect(result.current.kinds).toEqual([USER])
  })

  it("without a binding looks default up by id and probes two records for the rest", async () => {
    serve([account("a"), account("default"), account("c")])
    const withDefault = resolve(app())
    await waitFor(() =>
      expect(withDefault.result.current.states.me).toMatchObject({
        state: "default",
      })
    )
    const firsts = asked().map((a) =>
      new URL(a, "http://console").searchParams.get("first")
    )
    expect(firsts.sort()).toEqual(["1", "2"])

    serve([account("a"), account("b"), account("c")])
    const three = resolve(app())
    await waitFor(() =>
      expect(three.result.current.states.me).toEqual({ state: "ambiguous" })
    )
    serve([account("only")])
    const one = resolve(app())
    await waitFor(() =>
      expect(one.result.current.states.me).toMatchObject({ state: "sole" })
    )
    serve([])
    const none = resolve(app())
    await waitFor(() =>
      expect(none.result.current.states.me).toEqual({ state: "none" })
    )
  })

  it("settles a failed read as an error", async () => {
    serve([], 500)
    const { result } = resolve(app())
    await waitFor(() =>
      expect(result.current.states.me).toMatchObject({ state: "error" })
    )
  })
})
