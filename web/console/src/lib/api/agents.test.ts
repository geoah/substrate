/** The agent chat transport: one ndjson AgentEvent per line (thread, delta,
 * tool lifecycle, done), the transcript read-back that pulls the newest
 * assistant reply off a settled thread's messages, and how a provider row
 * reads on the agents page. */

import { afterEach, describe, expect, it, vi } from "vitest"

import {
  lastAssistantReply,
  parseAgentEvent,
  providerEndpoint,
  providerHasKey,
  streamChat,
} from "./agents"
import type { AgentEvent } from "./agents"
import type { SubstrateRecord } from "./types"

/** An ndjson stream response the fake fetch hands back. */
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

/** Drives streamChat to completion, collecting what each callback saw. */
function runStream(lines: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ndjson(lines))
  )
  const events: AgentEvent[] = []
  let error: string | undefined
  let done = false
  return new Promise<{ events: AgentEvent[]; error?: string; done: boolean }>(
    (resolve) => {
      streamChat({
        agent: "a",
        message: "hi",
        onEvent: (e) => events.push(e),
        onError: (e) => {
          error = e.message
          resolve({ events, error, done })
        },
        onDone: () => {
          done = true
          resolve({ events, error, done })
        },
      })
    }
  )
}

describe("parseAgentEvent", () => {
  it("reads a thread event", () => {
    expect(parseAgentEvent('{"kind":"thread","thread":"t-1"}')).toEqual({
      kind: "thread",
      thread: "t-1",
    })
  })
  it("reads a delta event", () => {
    expect(parseAgentEvent('{"kind":"delta","text":"hi"}')).toEqual({
      kind: "delta",
      text: "hi",
    })
  })
  it("reads a tool lifecycle event with ok", () => {
    expect(parseAgentEvent('{"kind":"toolFinished","tool":"query","ok":true}')).toEqual({
      kind: "toolFinished",
      tool: "query",
      ok: true,
    })
  })
  it("reads a done event carrying the result", () => {
    const ev = parseAgentEvent('{"kind":"done","result":{"reply":"ok","thread":"t-1","status":"ok","effects":0,"turns":1,"toolCalls":0,"promptTokens":1,"completionTokens":1,"totalTokens":2,"costUSD":0}}')
    expect(ev?.kind).toBe("done")
    expect(ev?.result?.status).toBe("ok")
  })
  it("returns null on blank lines, heartbeats and garbage", () => {
    expect(parseAgentEvent("")).toBeNull()
    expect(parseAgentEvent("   ")).toBeNull()
    expect(parseAgentEvent("{}")).toBeNull() // no kind
    expect(parseAgentEvent("not json")).toBeNull()
  })
})

describe("streamChat post-200 failures", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("routes an error event to onError and never settles onDone", async () => {
    const r = await runStream([
      '{"kind":"thread","thread":"t-1"}',
      '{"kind":"error","error":"loop blew up"}',
    ])
    expect(r.error).toBe("loop blew up")
    expect(r.done).toBe(false)
    // The error event is NOT forwarded as a normal event.
    expect(r.events.map((e) => e.kind)).toEqual(["thread"])
  })

  it("treats a done with text and no result as an error (older server)", async () => {
    const r = await runStream([
      '{"kind":"thread","thread":"t-1"}',
      '{"kind":"done","text":"the loop failed"}',
    ])
    expect(r.error).toBe("the loop failed")
    expect(r.done).toBe(false)
  })

  it("settles onDone on a clean run carrying a result", async () => {
    const r = await runStream([
      '{"kind":"thread","thread":"t-1"}',
      '{"kind":"delta","text":"hi"}',
      '{"kind":"done","result":{"reply":"hi","thread":"t-1","status":"ok","effects":0,"turns":1,"toolCalls":0,"promptTokens":1,"completionTokens":1,"totalTokens":2,"costUSD":0}}',
    ])
    expect(r.error).toBeUndefined()
    expect(r.done).toBe(true)
    expect(r.events.map((e) => e.kind)).toEqual(["thread", "delta", "done"])
  })
})

describe("lastAssistantReply", () => {
  const msg = (role: string, content: string): SubstrateRecord => ({
    id: `${role}-${content}`,
    kind: "core.substrate.reamde.dev/message",
    properties: { role, content },
    labels: {},
    version: 1,
    createdAt: "2026-08-08T00:00:00Z",
    updatedAt: "2026-08-08T00:00:00Z",
  })

  it("returns the newest assistant content, ignoring tool + user rows", () => {
    const messages = [
      msg("user", "hello"),
      msg("assistant", "first"),
      msg("tool", "result"),
      msg("assistant", "final"),
    ]
    expect(lastAssistantReply(messages)).toBe("final")
  })
  it("returns undefined when no assistant turn has prose", () => {
    expect(lastAssistantReply([msg("user", "hello")])).toBeUndefined()
  })
})

describe("provider rows", () => {
  const provider = (properties: Record<string, unknown>): SubstrateRecord => ({
    id: "default",
    kind: "core.substrate.reamde.dev/llmprovider",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-08-08T00:00:00Z",
    updatedAt: "2026-08-08T00:00:00Z",
  })

  it("reads an empty openai baseURL as the host's gateway", () => {
    expect(providerEndpoint(provider({ name: "default", wire: "openai" }))).toBe(
      "host gateway"
    )
  })
  it("reads an empty non-openai baseURL as that wire's own endpoint", () => {
    expect(providerEndpoint(provider({ wire: "anthropic", baseURL: "" }))).toBe(
      "default endpoint"
    )
  })
  it("reads an empty azure baseURL as missing, never as a default", () => {
    // An azure deployment has no host default — the loop refuses such a row.
    expect(providerEndpoint(provider({ wire: "azure", baseURL: "" }))).toBe(
      "missing baseURL"
    )
    expect(providerEndpoint(provider({ wire: "azure" }))).toBe("missing baseURL")
  })
  it("shows a declared baseURL verbatim", () => {
    const row = provider({ wire: "openai", baseURL: "https://example.com/v1" })
    expect(providerEndpoint(row)).toBe("https://example.com/v1")
  })

  it("reads a redacted apiKey as set, and an absent or empty one as not", () => {
    // The read surface redacts a secret, so only its presence is legible.
    expect(providerHasKey(provider({ apiKey: "<redacted>" }))).toBe(true)
    expect(providerHasKey(provider({ apiKey: "" }))).toBe(false)
    expect(providerHasKey(provider({}))).toBe(false)
  })
})
