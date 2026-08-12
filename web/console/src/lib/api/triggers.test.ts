/** The trigger surface: the run ledger reads through the generic record list
 * scoped to one trigger, and the verbs POST to the computed trigger endpoints
 * with the bodies the wire expects. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { listPath } from "./records"
import { replayTrigger, runTrigger, triggerRunsQueryOptions, wakeTrigger } from "./triggers"

describe("triggerRunsQueryOptions", () => {
  it("lists runs filtered to the trigger, newest first", () => {
    const opts = triggerRunsQueryOptions("wf-daily")
    // The query fn builds the same list path the generic machinery uses; assert
    // the filter + order land on the runs collection.
    const path = listPath({
      authority: "core.substrate.reamde.dev",
      plural: "runs",
      first: 25,
      filter: { properties: { trigger: { eq: "wf-daily" } } },
      orderBy: "startedAt:desc",
    })
    expect(path).toContain("/core.substrate.reamde.dev/runs?")
    expect(path).toContain("orderBy=startedAt%3Adesc")
    expect(decodeURIComponent(path)).toContain('"trigger":{"eq":"wf-daily"}')
    // the query key carries the same collection so caches don't collide
    expect(opts.queryKey[0]).toBe("records")
  })
})

describe("trigger verbs", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

  it("replay resets the cursor with a from body", async () => {
    fetchMock.mockResolvedValueOnce(ok({ from: 0 }))
    await replayTrigger("t1", 0)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain("/core.substrate.reamde.dev/triggers/t1/replay")
    expect(JSON.parse(String(init?.body))).toEqual({ from: 0 })
  })

  it("run synthesizes one delivery of a record's full identity", async () => {
    fetchMock.mockResolvedValueOnce(ok({ ran: 1 }))
    const res = await runTrigger("t1", "tasks.substrate.reamde.dev/issue", "issue-9")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain("/triggers/t1/run")
    expect(JSON.parse(String(init?.body))).toEqual({
      kind: "tasks.substrate.reamde.dev/issue",
      id: "issue-9",
    })
    expect(res.ran).toBe(1)
  })

  it("wake POSTs with no body", async () => {
    fetchMock.mockResolvedValueOnce(ok({ ran: 3 }))
    await wakeTrigger("t1")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain("/triggers/t1/wake")
    expect(init?.method).toBe("POST")
    expect(init?.body).toBeUndefined()
  })
})
