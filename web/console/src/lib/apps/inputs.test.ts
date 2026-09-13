// @vitest-environment jsdom
/** The three-step input rule and its one refusal: bound, then `default`,
 * then the sole record; a binding that names nothing the kind holds is
 * `missing` and never silently replaced. Each step is its own read by id or
 * a two-record probe, so a kind with more records than a page still
 * resolves, and a read that fails is an error the owner can act on. */

import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createElement, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { AppSpec } from "./spec"
import { inputStatus, resolveInput, useAppInputs } from "./inputs"

const KIND = "providers.example.com/github/account"
const account = (id: string): SubstrateRecord => ({
  id,
  kind: KIND,
  properties: {},
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

describe("resolveInput", () => {
  it("takes the binding first, by its own read", () => {
    const state = resolveInput(`${KIND}/work`, { bound: account("work") }, KIND)
    expect(state.state).toBe("bound")
    expect("record" in state && state.record.id).toBe("work")
    expect(resolveInput(`${KIND}/work`, {}, KIND)).toEqual({ state: "loading" })
  })
  it("then the record called default, then the sole record", () => {
    expect(
      resolveInput(undefined, { byDefault: account("default") }, KIND)
    ).toMatchObject({ state: "default" })
    expect(
      resolveInput(
        undefined,
        { byDefault: null, probe: [account("only")] },
        KIND
      )
    ).toMatchObject({ state: "sole" })
    expect(resolveInput(undefined, {}, KIND)).toEqual({ state: "loading" })
    expect(resolveInput(undefined, { byDefault: null }, KIND)).toEqual({
      state: "loading",
    })
  })
  it("is ambiguous with two and none with zero", () => {
    expect(
      resolveInput(
        undefined,
        { byDefault: null, probe: [account("a"), account("b")] },
        KIND
      )
    ).toMatchObject({ state: "ambiguous" })
    expect(
      resolveInput(undefined, { byDefault: null, probe: [] }, KIND)
    ).toEqual({ state: "none" })
  })
  it("keeps a binding that no longer resolves as missing, never a fallback", () => {
    const gone = resolveInput(
      `${KIND}/gone`,
      { bound: null, byDefault: account("default") },
      KIND
    )
    expect(gone).toMatchObject({ state: "missing", binding: `${KIND}/gone` })
    const wrongKind = resolveInput(
      "ada.example.com/people/person/default",
      {},
      KIND
    )
    expect(wrongKind.state).toBe("missing")
    expect(inputStatus("me", gone)).toMatch(/no longer exists/)
    expect(inputStatus("me", { state: "error", message: "boom" })).toMatch(
      /could not be read: boom/
    )
  })
})

// ── the hook: one read per step, never a page searched ──────────────────────

const accountKind: KindInfo = {
  identity: KIND,
  name: "account",
  authority: "providers.example.com",
  package: "github",
  version: 1,
  source: "installed",
  description: "",
  definition: { properties: { login: { type: "string" } } },
}

function app(bindings: Record<string, string> = {}): AppSpec {
  return {
    id: "gh",
    name: "GitHub",
    home: false,
    screens: [],
    inputs: { me: { kind: KIND } },
    bindings,
    problems: [],
  }
}

const COLLECTION = "/api/v1/providers.example.com/github/account"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function page(records: SubstrateRecord[], cursor?: string) {
  return { records, cursor, head: 0, generation: "g" }
}

/** Every request's path, so a test can say what was NOT fetched. */
let calls: string[] = []

function stubFetch(route: (path: string) => Response | undefined) {
  calls = []
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    const url = String(input)
    calls.push(url)
    return Promise.resolve(
      route(url) ??
        json({ error: { code: "not_found", message: "no such record" } }, 404)
    )
  })
}

function mount(spec: AppSpec) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children)
  return renderHook(() => useAppInputs(spec, [accountKind]), { wrapper })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("useAppInputs", () => {
  it("fetches a binding by id and nothing else", async () => {
    stubFetch((path) =>
      path === `${COLLECTION}/work` ? json(account("work")) : undefined
    )
    const { result } = mount(app({ me: `${KIND}/work` }))
    await waitFor(() => expect(result.current.states.me.state).toBe("bound"))
    expect(result.current.records.me?.id).toBe("work")
    expect(calls).toEqual([`${COLLECTION}/work`])
  })

  it("reads default by id, beyond any page", async () => {
    stubFetch((path) => {
      if (path === `${COLLECTION}/default`) return json(account("default"))
      if (path.startsWith(`${COLLECTION}?first=2`)) {
        return json(page([account("a"), account("b")], "more"))
      }
      return undefined
    })
    const { result } = mount(app())
    await waitFor(() => expect(result.current.states.me.state).toBe("default"))
    expect(calls.some((c) => c.includes("first=200"))).toBe(false)
  })

  it("keeps a default that resolved when the probe beside it failed first", async () => {
    stubFetch((path) => {
      if (path === `${COLLECTION}/default`) {
        // The probe's failure lands before default answers.
        return new Promise<Response>((resolve) =>
          setTimeout(() => resolve(json(account("default"))), 20)
        ) as unknown as Response
      }
      if (path.startsWith(`${COLLECTION}?first=2`)) {
        return json({ error: { code: "internal", message: "boom" } }, 500)
      }
      return undefined
    })
    const { result } = mount(app())
    await waitFor(() => expect(result.current.states.me.state).toBe("default"))
    expect(result.current.records.me?.id).toBe("default")
  })

  it("tells none, sole and ambiguous apart with two rows, and pages the picker apart", async () => {
    stubFetch((path) => {
      if (path.startsWith(`${COLLECTION}?first=2`)) {
        return json(page([account("a"), account("b")], "c1"))
      }
      if (path.startsWith(`${COLLECTION}?first=50`)) {
        return json(page([account("a"), account("b"), account("c")], "c2"))
      }
      return undefined
    })
    const { result } = mount(app())
    await waitFor(() =>
      expect(result.current.states.me).toMatchObject({
        state: "ambiguous",
        more: true,
      })
    )
    const state = result.current.states.me
    expect("options" in state && state.options.map((o) => o.id)).toEqual([
      "a",
      "b",
      "c",
    ])
    expect(calls.some((c) => c.includes("first=200"))).toBe(false)
  })

  it("is an error, not a loading state, when a read fails", async () => {
    stubFetch((path) =>
      path === `${COLLECTION}/default`
        ? json({ error: { code: "internal", message: "database away" } }, 500)
        : json(page([]))
    )
    const { result } = mount(app())
    await waitFor(() =>
      expect(result.current.states.me).toEqual({
        state: "error",
        message: "database away",
      })
    )
    expect(inputStatus("me", result.current.states.me)).toMatch(/database away/)
  })

  it("is missing when the bound id is gone, with the picker's page to rebind from", async () => {
    stubFetch((path) =>
      path.startsWith(`${COLLECTION}?first=50`)
        ? json(page([account("a")]))
        : undefined
    )
    const { result } = mount(app({ me: `${KIND}/gone` }))
    await waitFor(() =>
      expect(result.current.states.me).toMatchObject({
        state: "missing",
        options: [account("a")],
        more: false,
      })
    )
  })
})
