import { describe, expect, it } from "vitest"
import {
  SubstrateError,
  describe as words,
  failure,
  retryAfterSeconds,
} from "./errors"

describe("failure", () => {
  it("reads the REST envelope with its problems", async () => {
    const e = await failure(
      new Response(
        JSON.stringify({
          error: {
            code: "validation",
            message: "The record is not valid.",
            problems: ["name is required"],
            problemDetails: [{ path: "properties.name", message: "required" }],
          },
        }),
        { status: 422 }
      )
    )
    expect(e).toBeInstanceOf(SubstrateError)
    expect(e.code).toBe("validation")
    expect(e.status).toBe(422)
    expect(e.words).toBe("The record is not valid. name is required")
    expect(e.problemDetails[0].path).toBe("properties.name")
    expect(e.conflict).toBe(false)
    expect(e.transient).toBe(false)
  })

  it("reads an OAuth error", async () => {
    const e = await failure(
      new Response(
        JSON.stringify({
          error: "invalid_grant",
          error_description: "The code was used.",
        }),
        { status: 400 }
      )
    )
    expect(e.code).toBe("invalid_grant")
    expect(e.message).toBe("The code was used.")
  })

  it("says the status line for a body that is not JSON", async () => {
    const e = await failure(
      new Response("<html>", { status: 502, statusText: "Bad Gateway" })
    )
    expect(e.words).toBe("The server answered 502 Bad Gateway")
    expect(e.transient).toBe(true)
  })

  it("keeps Retry-After and an insufficient scope", async () => {
    const busy = await failure(
      new Response("{}", { status: 429, headers: { "Retry-After": "7" } })
    )
    expect(busy.retryAfter).toBe(7)
    expect(busy.transient).toBe(true)
    const scope = await failure(
      new Response(
        JSON.stringify({ error: { code: "forbidden", message: "no" } }),
        {
          status: 403,
          headers: { "WWW-Authenticate": 'Bearer error="insufficient_scope"' },
        }
      )
    )
    expect(scope.insufficientScope).toBe(true)
  })
})

describe("retryAfterSeconds", () => {
  it("reads seconds and an HTTP date", () => {
    const now = Date.parse("2026-11-08T12:00:00Z")
    expect(retryAfterSeconds("120", now)).toBe(120)
    expect(retryAfterSeconds("Sun, 08 Nov 2026 12:00:30 GMT", now)).toBe(30)
    expect(retryAfterSeconds("soon", now)).toBeUndefined()
    expect(retryAfterSeconds(null, now)).toBeUndefined()
  })
})

describe("describe", () => {
  it("says a network failure in the owner's words", () => {
    expect(words(new TypeError("Failed to fetch"))).toBe(
      "Couldn't reach the server. Check your connection and try again."
    )
  })
})
