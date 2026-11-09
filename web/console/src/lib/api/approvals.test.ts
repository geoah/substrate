/** An approval as the card reads it (decision record 0254): its state, the
 * agent and function by identity, and each argument labelled, with the
 * model's values as plain text. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { agentWord, approvalViewOf, argumentLabel } from "./approvals"
import { approvalNoticeOf, type TurnView } from "./transcript"

function approval(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "a1",
    kind: "substrate.reamde.dev/llm/approval",
    properties,
    labels: {},
    version: 3,
    createdAt: "2026-11-09T10:00:00Z",
    updatedAt: "2026-11-09T10:00:00Z",
  }
}

describe("approvalViewOf", () => {
  it("reads the call, the asker and every argument", () => {
    const view = approvalViewOf(
      approval({
        state: "pending",
        tool: "gmail_send",
        agent: {
          ref: "substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/secretary",
        },
        function: {
          ref: "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail",
        },
        requestedIn: { ref: "substrate.reamde.dev/llm/thread/child-1" },
        arguments: {
          to: ["Sam <sam@example.com>", "bea@example.com"],
          subject: "Dinner",
          body: "Friday at 8?\nAda",
          replyAll: false,
          html: "",
        },
      })
    )
    expect(view).toMatchObject({
      id: "a1",
      version: 3,
      state: "pending",
      tool: "gmail_send",
      agent: "substrate.reamde.dev/assistant/secretary",
      function: "providers.substrate.reamde.dev/google/sendmail",
      requestedIn: "child-1",
    })
    expect(view.arguments).toEqual([
      {
        name: "to",
        label: "To",
        value: "Sam <sam@example.com>, bea@example.com",
      },
      { name: "subject", label: "Subject", value: "Dinner" },
      { name: "body", label: "Body", value: "Friday at 8?\nAda" },
      { name: "replyAll", label: "Reply all", value: "No" },
    ])
  })

  it("reads an unknown state as pending and keeps a failure's reason", () => {
    expect(approvalViewOf(approval({ state: "weird" })).state).toBe("pending")
    expect(
      approvalViewOf(approval({ state: "failed", error: "HTTP 403" }))
    ).toMatchObject({ state: "failed", error: "HTTP 403" })
  })

  it("names an argument and an agent as a person reads them", () => {
    expect(argumentLabel("addAttendees")).toBe("Add attendees")
    expect(agentWord("substrate.reamde.dev/assistant/moss")).toBe("Moss")
  })
})

describe("approvalNoticeOf", () => {
  const turn = (content: string): TurnView =>
    ({ role: "system", content }) as unknown as TurnView

  it("decodes an approved call that ran, and one that did not", () => {
    expect(
      approvalNoticeOf(
        turn(
          JSON.stringify({
            event: "approvalDecided",
            approval: "substrate.reamde.dev/llm/approval/a1",
            decision: "approved",
            tool: "gmail_send",
            ok: true,
          })
        )
      )
    ).toEqual({
      approvalId: "a1",
      decision: "approved",
      tool: "gmail_send",
      ok: true,
    })
    expect(
      approvalNoticeOf(
        turn(
          JSON.stringify({
            event: "approvalDecided",
            approval: "substrate.reamde.dev/llm/approval/a2",
            decision: "approved",
            tool: "gmail_send",
            ok: false,
            error: "google answered HTTP 403",
          })
        )
      )
    ).toMatchObject({ ok: false, error: "google answered HTTP 403" })
  })

  it("ignores any other system row", () => {
    expect(approvalNoticeOf(turn('{"event":"toolCompleted"}'))).toBeUndefined()
    expect(approvalNoticeOf(turn("plain text"))).toBeUndefined()
  })
})
