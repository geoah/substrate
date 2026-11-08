import { describe, expect, it } from "vitest"
import { deriveLinkStatus, statusSentence } from "./status"

const words = {
  app: "Substrate Template",
  device: "this browser",
  kept: "items",
}
const pkg = { id: "substrate.reamde.dev/template", name: "Template", needs: 2 }

describe("the link status", () => {
  it("puts what blocks most first", () => {
    expect(
      deriveLinkStatus({
        packages: [{ ...pkg, held: 1 }],
        outbox: { type: "offline", words: "x" },
        waiting: 2,
        issues: [{ type: "accessEnded" }],
      })
    ).toEqual({ type: "signedOut" })
    expect(
      deriveLinkStatus({
        packages: [{ ...pkg, held: 2 }],
        outbox: { type: "offline", words: "You're offline." },
        waiting: 2,
        issues: [],
      })
    ).toEqual({ type: "offline", waiting: 2, reason: "You're offline." })
    expect(
      deriveLinkStatus({
        packages: null,
        outbox: { type: "idle" },
        waiting: 0,
        issues: [],
      })
    ).toEqual({ type: "preparing", names: [] })
  })

  it("says each in the kit's words", () => {
    expect(statusSentence({ type: "ready", waiting: 0 }, words)).toBe(
      "Up to date."
    )
    expect(statusSentence({ type: "ready", waiting: 1 }, words)).toBe(
      "Sending 1 change."
    )
    expect(
      statusSentence({ type: "offline", waiting: 3, reason: "" }, words)
    ).toBe("Your substrate cannot be reached. 3 changes wait in this browser.")
    expect(
      statusSentence({ type: "refused", waiting: 1, reason: "" }, words)
    ).toBe(
      "Your substrate refused a change. 1 change waits; retry or discard it below."
    )
    expect(
      statusSentence(
        { type: "packagesBehind", packages: [{ ...pkg, held: 1 }] },
        words
      )
    ).toBe(
      "Your substrate has version 1 of the Template package and this app needs version 2 or later. The update waits for your approval on your substrate; items wait in this browser until then."
    )
  })
})
