import { describe, expect, it } from "vitest"
import { targetFor } from "../engine/curves"
import { computeReport, yearsAt } from "../engine/engine"
import { focusList } from "../engine/focus"
import { contributorSegments } from "../healthspan/segmentTones"
import { demoSeries } from "./healthspan"
import { DEMO_PERSON } from "./shared"

const today = new Date("2026-10-25T12:00:00Z")
const ME = { birthDate: DEMO_PERSON.birthDate, sex: DEMO_PERSON.sex }

describe("the demo member", () => {
  const fit = demoSeries(today)
  const r = computeReport(fit.input, ME, today)
  const snap = r.snapshots.at(-1)

  it("has data for all nine contributors", () => {
    expect(snap?.contributors).toBe(9)
  })

  it("keeps the band's nights over their copy through the phone", () => {
    const sources = new Set(fit.input.sleeps.map((s) => s.source))
    expect([...sources]).toEqual(["band/demo"])
    expect(fit.nightOrder.slice(0, 2)).toEqual(["band/demo", "phone/demo/band"])
  })

  it("reads scans from two sources and a VO2 max every 20 days", () => {
    expect(new Set(fit.lean.map((l) => l.source))).toEqual(
      new Set(["lab/demo-dxa", "scale/demo"])
    )
    expect(fit.vo2).toHaveLength(10)
  })

  it("gives a stable pace inside the scale every week", () => {
    expect(r.snapshots.length).toBeGreaterThan(20)
    for (const s of r.snapshots) {
      expect(s.pace).toBeGreaterThanOrEqual(-1)
      expect(s.pace).toBeLessThanOrEqual(3)
    }
  })

  it("has strength slipping at the head of the focus list", () => {
    expect(focusList(snap?.metrics ?? [])[0]?.id).toBe("strength")
  })

  it("colours the segment under each 30-day marker as its badge says", () => {
    for (const state of [undefined, "calibrating", "paused"] as const) {
      const rep = computeReport(demoSeries(today, state).input, ME, today)
      const sn = rep.snapshots.at(-1)
      for (const m of sn?.metrics ?? []) {
        if (m.thirtyDay === null || m.impact30 === null) continue
        const segs = contributorSegments(
          m,
          targetFor(m.id, sn?.age ?? 0, "male")
        )
        const w = (m.scale.max - m.scale.min) / segs.length
        const i = Math.max(
          0,
          Math.min(segs.length - 1, Math.floor((m.thirtyDay - m.scale.min) / w))
        )
        expect(segs[i].tone === "warning", `${state} ${m.id}`).toBe(
          m.impact30 >= 0.05
        )
      }
    }
  })

  it("never puts a 30-day marker on orange without a badge, anywhere on any bar", () => {
    for (const m of snap?.metrics ?? []) {
      const t = targetFor(m.id, snap?.age ?? 0, "male")
      const { min, max, segments } = m.scale
      const w = (max - min) / segments.length
      for (let k = 0; k <= 400; k++) {
        const v = min + ((max - min) * k) / 400
        const segs = contributorSegments({ ...m, thirtyDay: v }, t)
        const i = Math.max(
          0,
          Math.min(segments.length - 1, Math.floor((v - min) / w))
        )
        expect(segs[i].tone === "warning", `${m.id} at ${v}`).toBe(
          yearsAt(m.curve, v) >= 0.05
        )
      }
    }
  })

  it("reaches the calibrating, paused and locked states by knob", () => {
    const st = (state: "calibrating" | "paused" | "locked") =>
      computeReport(demoSeries(today, state).input, ME, today)
    expect(st("calibrating").snapshots.at(-1)?.calibrating).toBe(true)
    expect(st("paused").paused?.recoveries).toBeLessThan(21)
    expect(st("paused").snapshots.length).toBeGreaterThan(0)
    expect(st("locked").snapshots).toEqual([])
  })

  it("has one VO2 max reading in the onevo2 state", () => {
    expect(demoSeries(today, "onevo2").vo2).toHaveLength(1)
  })
})
