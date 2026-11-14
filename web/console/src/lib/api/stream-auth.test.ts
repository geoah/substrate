// @vitest-environment jsdom
/** A stream whose token ends (revoked, or the person signed out everywhere)
 * ends the session, as a 401 does: the watch on an `auth` terminal frame, a
 * chat on an `error` event whose code is `auth`. Neither opens again. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { streamChat } from "./agents"
import { watchChanges, type WatchStatus } from "./changes"
import { nothingSaved } from "./compacted"
import { getToken, setToken, setUnauthorizedHandler } from "./session"
import { ApiError } from "./types"

function ndjson(lines: string[]): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const enc = new TextEncoder()
      for (const l of lines) controller.enqueue(enc.encode(l + "\n"))
      controller.close()
    },
  })
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/x-ndjson" },
  })
}

describe("a stream whose token ended", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setUnauthorizedHandler(null)
  })

  it("ends the session on a watch's auth frame, and opens no stream again", async () => {
    setToken("substrate_tok_revoked")
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    const fetchMock = vi.fn(async () =>
      ndjson([
        '{"bookmark":3,"generation":"g1"}',
        '{"error":{"code":"auth","message":"the token was revoked"}}',
      ])
    )
    vi.stubGlobal("fetch", fetchMock)
    const statuses: WatchStatus[] = []
    const handle = watchChanges({
      reader: { key: "tail", drop: nothingSaved, fail: () => {} },
      onRow: () => {},
      onStatus: (s) => statuses.push(s),
      onCompacted: () => {},
    })
    await vi.waitFor(() => expect(statuses.at(-1)).toBe("off"))
    expect(unauthorized).toHaveBeenCalled()
    expect(getToken()).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    handle.stop()
  })

  it("ends the session on a chat's auth error event", async () => {
    setToken("substrate_tok_revoked")
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        ndjson([
          '{"kind":"thread","thread":"t-1"}',
          '{"kind":"error","code":"auth","error":"the token was revoked"}',
        ])
      )
    )
    const error = await new Promise<unknown>((resolve) => {
      streamChat({
        agent: "a",
        message: "hi",
        onEvent: () => {},
        onError: resolve,
        onDone: () => resolve("settled"),
      })
    })
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe("auth")
    expect(unauthorized).toHaveBeenCalled()
    expect(getToken()).toBeNull()
  })
})
