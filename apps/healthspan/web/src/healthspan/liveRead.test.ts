import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate } from "@substrate/testing"
import { afterEach, describe, expect, it, vi } from "vitest"
import { readHealthspan } from "../data/healthspan"
import { demoHealthspanRaw } from "../demo/healthspan"
import { DEMO_PERSON, rec } from "../demo/shared"
import { computeReport } from "../engine/engine"
import { KIND } from "../kinds"
import { toHealthspan } from "./read"

const today = new Date(2026, 9, 25)
const MET = "substrate.reamde.dev/observations/metric/"
const ME = { birthDate: DEMO_PERSON.birthDate, sex: DEMO_PERSON.sex }

/** The demo member's records in a fake substrate, with the metric records
 * a repository holding them declares, and the day reads answered as the
 * demo answers them. */
function seeded(declare: string[]): FakeSubstrate {
  const fake = new FakeSubstrate()
  const raw = demoHealthspanRaw(today)
  for (const r of [...raw.sources, ...raw.sessions, ...raw.rhr, ...raw.body])
    fake.seed(r.kind, r.id, r.properties)
  for (const id of declare)
    fake.seed(
      KIND.metric,
      id,
      rec(KIND.metric, id, {
        name: id,
        category: id.startsWith("health/body") ? "body" : "fitness",
      }).properties
    )
  const metric = (q: { aggregate: { metric?: { ref: string } } }) =>
    q.aggregate.metric?.ref.slice(MET.length)
  fake.answerAggregate((q) => !!q.aggregate.metrics, raw.signals)
  fake.answerAggregate((q) => metric(q) === "health/steps", raw.steps)
  fake.answerAggregate((q) => metric(q) === "health/vo2_max", raw.vo2)
  fake.answerAggregate((q) => metric(q) === "health/hrv_sdnn", raw.hrvSdnn)
  fake.answerAggregate(
    (q) => metric(q) === "health/height",
    raw.height ?? raw.vo2
  )
  return fake
}

afterEach(() => vi.unstubAllGlobals())

describe("the live Healthspan read", () => {
  it("reads into the same series the demo makes, and the same score", async () => {
    const fake = seeded([
      "whoop/recovery",
      "whoop/strain",
      "whoop/sleep_performance",
      "health/resting_heart_rate",
      "health/hrv_rmssd",
      "health/hrv_sdnn",
      "health/respiratory_rate",
      "health/spo2",
      "health/skin_temperature",
      "health/steps",
      "health/vo2_max",
      "health/height",
      "health/body_mass",
      "health/body_fat_fraction",
      "health/fat_free_mass",
      "health/visceral_fat_rating",
    ])
    vi.stubGlobal("fetch", fake.fetch)
    const raw = await readHealthspan(
      new SubstrateClient(fake.origin, "tok"),
      today
    )
    const live = toHealthspan(raw, {})
    const local = toHealthspan(demoHealthspanRaw(today), {})
    expect(live.input.sleeps.length).toBe(local.input.sleeps.length)
    expect(live.input.workouts.length).toBe(local.input.workouts.length)
    expect(live.input.rhr.length).toBe(local.input.rhr.length)
    expect(live.lean).toEqual(local.lean)
    expect(live.heightM).toBe(1.8)
    const a = computeReport(live.input, ME, today).snapshots.at(-1)
    const b = computeReport(local.input, ME, today).snapshots.at(-1)
    expect(a?.ageScore).toBeCloseTo(b?.ageScore ?? 0, 9)

    // Each aggregate names no source and no order: the repository ranks.
    const reads = fake.aggregates.map((q) => q.aggregate)
    expect(reads.length).toBe(5)
    for (const q of reads.filter((x) => !x.latest)) {
      expect(q.sources).toEqual({ policy: "priority" })
      expect(q.range?.recordedLocal).toBe(true)
    }
    expect(JSON.stringify(fake.aggregates)).not.toMatch(/"source"/)
    const signals = reads.find((q) => q.metrics)
    expect(signals?.bucket?.place).toBe("end")
    expect(signals?.metrics?.map((m) => m.ref.slice(MET.length))).toEqual([
      "whoop/recovery",
      "whoop/strain",
      "whoop/sleep_performance",
      "health/resting_heart_rate",
      "health/hrv_rmssd",
      "health/respiratory_rate",
      "health/spo2",
      "health/skin_temperature",
    ])
  })

  it("names only the metrics the repository declares", async () => {
    const fake = seeded(["health/resting_heart_rate", "health/steps"])
    vi.stubGlobal("fetch", fake.fetch)
    const raw = await readHealthspan(
      new SubstrateClient(fake.origin, "tok"),
      today
    )
    const reads = fake.aggregates.map((q) => q.aggregate)
    expect(reads).toHaveLength(2)
    expect(reads[0].metrics?.map((m) => m.ref.slice(MET.length))).toEqual([
      "health/resting_heart_rate",
    ])
    expect(raw.vo2.rows).toEqual([])
    expect(raw.body).toEqual([])
  })
})
