/** Reading an alert off its record, and the place in the console it lives:
 * the agent's or the tool's page its `about` names. */

import { describe, expect, it } from "vitest"

import {
  alertPlace,
  callablePaths,
  detailLine,
  readAlert,
  ALERT_KIND,
} from "./alerts"
import type { SubstrateRecord } from "./api/types"

const CORE = "substrate.reamde.dev/core"

function alert(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "trigger.parked/on-gmail",
    kind: ALERT_KIND,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-10T10:00:00Z",
    updatedAt: "2026-10-10T10:00:00Z",
  }
}

describe("readAlert", () => {
  it("reads the declared properties and the about references as paths", () => {
    const a = readAlert(
      alert({
        key: "trigger.parked/on-gmail",
        level: "error",
        state: "open",
        summary: "Trigger on-gmail has parked deliveries",
        detail: "egress blocked\nTraceback",
        count: 3,
        firstSeenAt: "2026-10-10T08:00:00Z",
        lastSeenAt: "2026-10-10T09:55:00Z",
        about: [
          { ref: `${CORE}/trigger/on-gmail` },
          {
            ref: `${CORE}/function/providers.substrate.reamde.dev/google/syncgmail`,
          },
        ],
      })
    )
    expect(a).toMatchObject({
      key: "trigger.parked/on-gmail",
      level: "error",
      open: true,
      count: 3,
      about: [
        `${CORE}/trigger/on-gmail`,
        `${CORE}/function/providers.substrate.reamde.dev/google/syncgmail`,
      ],
    })
    expect(detailLine(a)).toBe("egress blocked")
  })

  it("reads an undeclared level as error and a resolved state as not open", () => {
    const a = readAlert(alert({ level: "loud", state: "resolved" }))
    expect(a.level).toBe("error")
    expect(a.open).toBe(false)
    expect(a.summary).toBe("trigger.parked/on-gmail")
  })
})

describe("alertPlace", () => {
  it("is the tool page of the function the alert is about", () => {
    const a = readAlert(
      alert({
        about: [
          { ref: `${CORE}/trigger/on-gmail` },
          {
            ref: `${CORE}/function/providers.substrate.reamde.dev/google/syncgmail`,
          },
        ],
      })
    )
    expect(alertPlace(a)).toEqual({
      to: "tool",
      authority: "providers.substrate.reamde.dev",
      pkg: "google",
      name: "syncgmail",
    })
  })

  it("is the agent page of the agent the alert is about", () => {
    const a = readAlert(
      alert({ about: [{ ref: `${CORE}/agent/ada.example.com/notes/titler` }] })
    )
    expect(alertPlace(a)).toEqual({
      to: "agent",
      id: "ada.example.com/notes/titler",
      name: "titler",
    })
  })

  it("is nothing when the alert names no callable", () => {
    const a = readAlert(alert({ about: [{ ref: `${CORE}/trigger/on-gmail` }] }))
    expect(alertPlace(a)).toBeUndefined()
  })
})

describe("callablePaths", () => {
  it("names each function and agent of a package by its record path", () => {
    expect(
      callablePaths(
        ["providers.substrate.reamde.dev/google/syncgmail"],
        ["providers.substrate.reamde.dev/google/triage"]
      )
    ).toEqual([
      `${CORE}/function/providers.substrate.reamde.dev/google/syncgmail`,
      `${CORE}/agent/providers.substrate.reamde.dev/google/triage`,
    ])
  })
})
