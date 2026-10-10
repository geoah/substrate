import { describe, expect, it } from "vitest"

import type { TriggerStatus } from "@/lib/api/types"
import { failingTriggersOf } from "@/lib/health"

const BOOM = "widgets.test.dev/widgets/boom"
const TITLER = "ada.example.com/notes/titler"

function status(
  id: string,
  callable: string,
  health: string,
  failingSince?: string
): TriggerStatus {
  return {
    id,
    kind: "record",
    callable,
    enabled: true,
    head: 10,
    parked: 0,
    pending: 0,
    inFlight: 0,
    health,
    failingSince,
  }
}

describe("failingTriggersOf", () => {
  it("is undefined while every trigger of the callables is healthy", () => {
    expect(
      failingTriggersOf(
        [
          status("on-boom", BOOM, "ok"),
          status("on-titler", TITLER, "failing", "2026-10-10T08:00:00Z"),
        ],
        [BOOM]
      )
    ).toBeUndefined()
  })

  it("names the failing triggers of the callables and the oldest failingSince", () => {
    expect(
      failingTriggersOf(
        [
          status("on-boom-hourly", BOOM, "failing", "2026-10-10T09:00:00Z"),
          status("on-boom", BOOM, "failing", "2026-10-10T07:30:00Z"),
          status("on-titler", TITLER, "failing", "2026-10-09T00:00:00Z"),
          status("on-boom-ok", BOOM, "ok"),
        ],
        [BOOM]
      )
    ).toEqual({
      triggers: ["on-boom", "on-boom-hourly"],
      since: "2026-10-10T07:30:00Z",
    })
  })

  it("reads a failing trigger with no failingSince as failing with no instant", () => {
    expect(
      failingTriggersOf([status("on-boom", BOOM, "failing")], [BOOM, TITLER])
    ).toEqual({ triggers: ["on-boom"], since: undefined })
  })
})
