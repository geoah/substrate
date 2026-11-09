import { Effect } from "effect"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { KIND } from "../../../kinds"
import { Server, VirtualClock, browser } from "../../harness"
import { recordId } from "../../ids"
import { KINDS, sourceKey } from "../shared"
import { fakeContext, inputFromRecord } from "../testing"
import type { Plan, RecordKey } from "../types"
import { AMENDED_AFTER, NEWER, OLDER, REVIEWED_TEST } from "./fixtures"
import { numan } from "./index"

// The importer's plans written end to end by the one runner, against the
// fake substrate: a later export updates what changed, and an older one
// replayed over it is stale.

let server: Server

beforeEach(() => {
  server = new Server(new VirtualClock())
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const plan = (files: Record<string, string>): Promise<Plan> =>
  Effect.runPromise(
    numan.read(inputFromRecord("Numan exports", files), fakeContext())
  )

async function importPlan(id: string, p: Plan) {
  const one = browser(server, "a")
  expect(await one.stage({ id, plan: p })).toEqual({ state: "planned" })
  return one.resume(id)
}

const live = (k: RecordKey) => server.live(k.kind, recordId(k))
const result = (key: string) =>
  live({ kind: KINDS.labresult, namespace: "numan", key })

describe("Numan's exports, written", () => {
  it("creates every record of the first, updates what a later one changed, and leaves an older replay stale", async () => {
    const older = await plan({ "alex-numan-20260701.json": OLDER })
    const first = await importPlan(
      "0194a1b2-0000-7000-8000-0000000000a1",
      older
    )
    expect(first).toMatchObject({
      state: "imported",
      counts: {
        created: older.records.length,
        updated: 0,
        unchanged: 0,
        stale: 0,
      },
    })
    expect(live(sourceKey("numan"))?.properties).toMatchObject({
      name: "Numan",
      class: "service",
    })
    const ferritin = `${AMENDED_AFTER.shortId}/Ferritin`
    expect(result(ferritin)?.properties.value).toMatchObject({ value: "80" })

    const both = await plan({
      "alex-numan-20260701.json": OLDER,
      "alex-numan-20261001.json": NEWER,
    })
    const second = await importPlan(
      "0194a1b2-0000-7000-8000-0000000000b2",
      both
    )
    // the amended report and result, and the re-reviewed report's file
    expect(second).toMatchObject({
      state: "imported",
      counts: { updated: 3, stale: 0 },
    })
    expect(result(ferritin)?.properties.value).toMatchObject({ value: "85" })
    const reviewed = live({
      kind: KIND.labreport,
      namespace: "numan",
      key: REVIEWED_TEST.shortId,
    })
    const file = reviewed?.properties.file
    expect(file).toBeDefined()

    const replay = await importPlan(
      "0194a1b2-0000-7000-8000-0000000000c3",
      older
    )
    // the amended test's report and result are stale; the re-reviewed
    // test has the same revision in both, so the replay writes its older
    // review back, as a revision no higher than the plan's is updated
    expect(replay).toMatchObject({
      state: "imported",
      counts: { created: 0, updated: 1, stale: 2 },
    })
    expect(result(ferritin)?.properties.value).toMatchObject({ value: "85" })
    expect(
      live({
        kind: KIND.labreport,
        namespace: "numan",
        key: REVIEWED_TEST.shortId,
      })?.properties.file
    ).not.toEqual(file)
  })
})
