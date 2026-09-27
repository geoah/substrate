// @vitest-environment jsdom
/** The sync summary's error box: shown while the error still describes the
 * sync, hidden once a later run finished. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { SyncSummary } from "./sync-panel"
import { syncFieldsOf } from "@/lib/sync"

afterEach(cleanup)

const ERROR = "upstream returned HTTP 403"

function summary(properties: Record<string, unknown>) {
  render(<SyncSummary fields={syncFieldsOf(properties)} />)
}

describe("SyncSummary last error", () => {
  it("hides an error older than the last finished run", () => {
    summary({
      syncState: "ok",
      lastSyncedAt: "2026-09-26T10:00:00Z",
      syncError: ERROR,
      syncErrorAt: "2026-09-20T10:00:00Z",
    })
    expect(screen.queryByText("Last error")).toBeNull()
    expect(screen.queryByText(ERROR)).toBeNull()
  })

  it("shows an error newer than the last finished run", () => {
    summary({
      syncState: "throttled",
      lastSyncedAt: "2026-09-20T10:00:00Z",
      syncError: ERROR,
      syncErrorAt: "2026-09-26T10:00:00Z",
    })
    expect(screen.getByText("Last error")).toBeTruthy()
    expect(screen.getByText(ERROR)).toBeTruthy()
  })

  it("shows the error while the state is erroring, however old", () => {
    summary({
      syncState: "erroring",
      lastSyncedAt: "2026-09-26T10:00:00Z",
      syncError: ERROR,
      syncErrorAt: "2026-09-20T10:00:00Z",
    })
    expect(screen.getByText("Last error")).toBeTruthy()
    expect(screen.getByText(ERROR)).toBeTruthy()
  })

  it("shows an error when no run has finished since", () => {
    summary({
      syncState: "ok",
      syncError: ERROR,
      syncErrorAt: "2026-09-20T10:00:00Z",
    })
    expect(screen.getByText(ERROR)).toBeTruthy()
  })
})
