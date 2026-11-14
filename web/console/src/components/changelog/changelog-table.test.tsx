// @vitest-environment jsdom
/** The changelog table's tail under RULE W, against the real stream: its
 * failure is shown (the table says History didn't load, with Try again,
 * and the tail `stopped`), and the minute belongs to what the tail follows,
 * not to a mount, so a table mounted again within it sends nothing from the
 * history a refusal dropped, and a refusal there is the failure. */

import { useState, type ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="#">{children}</a>,
}))

import { ChangelogTable } from "./changelog-table"
import type { WatchStatus } from "@/lib/api/changes"
import { forgetRecoveries } from "@/lib/api/compacted"

const TASK = "acme.example.com/tasks/task"

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })

const encoder = new TextEncoder()
const line = (value: unknown) => encoder.encode(JSON.stringify(value) + "\n")

interface Tail {
  from: string | null
  generation: string | null
}

/** A server whose history `replace` replaces: each changelog read answers
 * the current generation (a row at 7 under g1, then 9 under g2, …), a tail
 * that resumes a position under an older one is refused, and a replacement
 * ends every open tail with a `compacted` frame. */
function server() {
  let generation = 1
  const head = () => 5 + 2 * generation
  const tails: Tail[] = []
  const open = new Set<ReadableStreamDefaultController<Uint8Array>>()
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), "http://x")
      const q = url.searchParams
      if (!url.pathname.endsWith("/changes"))
        return json({ records: [], head: 1, generation: "g" })
      if (!q.has("watch"))
        return json({
          changes: [
            {
              seq: head(),
              ts: new Date().toISOString(),
              actor: "act-person",
              op: "put",
              kind: TASK,
              recordId: `t${head()}`,
            },
          ],
          head: head(),
          generation: `g${generation}`,
        })
      tails.push({ from: q.get("from"), generation: q.get("generation") })
      if (q.has("from") && q.get("generation") !== `g${generation}`)
        return json(
          {
            error: {
              code: "compacted",
              message: `seq ${q.get("from")} is not in this history`,
            },
          },
          410
        )
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          open.add(controller)
          init?.signal?.addEventListener("abort", () => {
            open.delete(controller)
            controller.error(new DOMException("stopped", "AbortError"))
          })
          controller.enqueue(
            line({ bookmark: head(), generation: `g${generation}` })
          )
        },
      })
      return new Response(body, {
        status: 200,
        headers: { "content-type": "application/x-ndjson" },
      })
    })
  )
  const replace = () => {
    generation++
    for (const controller of open)
      controller.enqueue(
        line({
          error: { code: "compacted", message: "the history was replaced" },
        })
      )
    open.clear()
  }
  return { tails, replace }
}

/** The table, with its tail's state shown as the History panel's follow
 * toggle shows it. */
function Table() {
  const [status, setStatus] = useState<WatchStatus>("off")
  return (
    <>
      <p data-testid="tail">{status}</p>
      <ChangelogTable filter={{}} follow kinds={[]} onStatus={setStatus} />
    </>
  )
}

describe("the changelog table's tail", () => {
  let client: QueryClient
  beforeEach(() => {
    vi.useFakeTimers()
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  })
  afterEach(() => {
    cleanup()
    client.clear()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    forgetRecoveries()
  })

  const mount = () =>
    render(
      <QueryClientProvider client={client}>
        <Table />
      </QueryClientProvider>
    )
  const step = async (ms: number) => {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms)
    })
    for (let i = 0; i < 10; i++)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
  }
  const replaced = async (replace: () => void) => {
    await act(async () => {
      replace()
      await vi.advanceTimersByTimeAsync(0)
    })
    await step(0)
  }
  const tail = () => screen.getByTestId("tail").textContent

  it("shows the failure: History didn't load, with Try again, and the tail stopped", async () => {
    const { replace } = server()
    mount()
    await step(0)
    expect(tail()).toBe("live")
    // Replaced at T0: the recovery. Replaced again 30 s on: the failure.
    await replaced(replace)
    expect(tail()).toBe("live")
    await step(30_000)
    await replaced(replace)
    expect(tail()).toBe("stopped")
    expect(screen.getByText("History didn’t load")).toBeTruthy()
    expect(screen.getByText("the history was replaced")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
  })

  it("sends nothing from the refused history when mounted again within the minute, and fails there", async () => {
    const { tails, replace } = server()
    const first = mount()
    await step(0)
    await replaced(replace)
    await step(30_000)
    // The pages read for the recovery are of g2; replaced again, g2 is
    // refused, and so is every position read under it.
    await replaced(replace)
    expect(tail()).toBe("stopped")
    const opened = tails.length
    first.unmount()
    await step(5_000)
    mount()
    await step(0)
    const again = tails.slice(opened)
    expect(again.filter((t) => t.generation === "g2")).toEqual([])
    expect(tail()).toBe("live")
    // Replaced 40 s after the recovery started: the failure, shown.
    await step(5_000)
    await replaced(replace)
    expect(tail()).toBe("stopped")
    expect(screen.getByText("History didn’t load")).toBeTruthy()
    expect(tails).toHaveLength(opened + again.length)
  })
})
