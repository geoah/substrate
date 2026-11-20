import { Effect } from "effect"
import { describe, expect, it } from "vitest"
import { pdfFixture, runResult } from "../testing"
import { SHEET_RUNS, sheetPdf, type SheetRun } from "./fixtures"
import { readSheet, type Sheet } from "./tanita"

const PATH = "files/report/sheet.pdf"

const sheet = (runs: readonly SheetRun[] = SHEET_RUNS): Promise<Sheet> =>
  Effect.runPromise(readSheet(sheetPdf(runs), PATH))

const failure = async (runs: readonly SheetRun[]) => {
  const r = await runResult(readSheet(sheetPdf(runs), PATH))
  return r._tag === "Failure" ? r.failure : undefined
}

/** The sheet's runs with one changed or removed. */
const without = (text: string, font?: string) =>
  SHEET_RUNS.filter((r) => !(r[3] === text && (!font || r[2] === font)))

describe("a body composition sheet", () => {
  it("is read by its labels' positions", async () => {
    const s = await sheet()
    expect(s.id).toBe("4242")
    expect(s.model).toBe("MC-780")
    expect(s.at).toBe("2026-02-10T09:20:00+00:00")
    expect(s.minute).toBe("2026-02-10T09:20")
    expect(
      s.rows.map((r) =>
        [
          r.name,
          r.measure,
          r.channel ?? "",
          r.value,
          r.unit,
          r.low ?? "",
          r.high ?? "",
        ].join(" | ")
      )
    ).toEqual([
      "Weight | Weight |  | 81.6 | kg | 68.0 | 83.0",
      "Fat % | Fat % |  | 22.4 | % | 11.0 | 21.9",
      "Fat Mass | Fat Mass |  | 18.3 | kg |  | ",
      "FFM | FFM |  | 63.3 | kg |  | ",
      "Muscle Mass | Muscle Mass |  | 60.1 | kg | 52.0 | 66.0",
      "BMI | BMI |  | 25.2 | kg/m2 | 18.5 | 24.9",
      "Metabolic Age | Metabolic Age |  | 41 | a |  | ",
      "BMR | BMR |  | 1795 | kcal/d |  | ",
      "Visceral Fat Rating | Visceral Fat Rating |  | 8 | {score} |  | ",
      "TBW | TBW |  | 45.0 | kg |  | ",
      "TBW % | TBW % |  | 55.1 | % |  | ",
      "ECW | ECW |  | 19.2 | kg |  | ",
      "ICW | ICW |  | 25.8 | kg |  | ",
      "ECW/TBW | ECW/TBW |  | 42.7 | % |  | ",
      "Bone Mass | Bone Mass |  | 3.3 | kg |  | ",
      "Protein | Protein |  | 12.4 | kg |  | ",
      "Leg Muscle Score | Leg Muscle Score |  | 95 | {score} |  | ",
      "Muscle Mass Trunk | Muscle Mass | trunk | 31.2 | kg |  | ",
      "Fat % Trunk | Fat % | trunk | 24.1 | % |  | ",
      "Fat Mass Trunk | Fat Mass | trunk | 10.2 | kg |  | ",
      "Muscle Mass Left Arm | Muscle Mass | left_arm | 3.4 | kg |  | ",
      "Fat % Left Arm | Fat % | left_arm | 19.8 | % |  | ",
      "Fat Mass Left Arm | Fat Mass | left_arm | 0.9 | kg |  | ",
      "Muscle Mass Right Arm | Muscle Mass | right_arm | 3.5 | kg |  | ",
      "Fat % Right Arm | Fat % | right_arm | 19.5 | % |  | ",
      "Fat Mass Right Arm | Fat Mass | right_arm | 0.9 | kg |  | ",
      "Muscle Mass Left Leg | Muscle Mass | left_leg | 10.6 | kg |  | ",
      "Fat % Left Leg | Fat % | left_leg | 18.7 | % |  | ",
      "Fat Mass Left Leg | Fat Mass | left_leg | 2.6 | kg |  | ",
      "Muscle Mass Right Leg | Muscle Mass | right_leg | 10.8 | kg |  | ",
      "Fat % Right Leg | Fat % | right_leg | 18.9 | % |  | ",
      "Fat Mass Right Leg | Fat Mass | right_leg | 2.7 | kg |  | ",
      "Phase Angle H-L (50 kHz) | Phase Angle |  | 6.2 | deg |  | ",
      "Phase Angle Right Leg | Phase Angle | right_leg | 6.5 | deg |  | ",
      "Phase Angle Left Leg | Phase Angle | left_leg | 6.4 | deg |  | ",
      "Phase Angle Right Arm | Phase Angle | right_arm | 5.9 | deg |  | ",
      "Phase Angle Left Arm | Phase Angle | left_arm | 5.8 | deg |  | ",
      "Phase Angle Leg-Leg | Phase Angle | leg_to_leg | 7.1 | deg |  | ",
    ])
  })

  it("prints its time on the clinic's clock, at the summer offset in summer", async () => {
    const summer = SHEET_RUNS.map((r): SheetRun =>
      r[3] === "10/02/2026 09:20" ? [r[0], r[1], r[2], "15/07/2025 08:45"] : r
    )
    const s = await sheet(summer)
    expect(s.at).toBe("2025-07-15T08:45:00+01:00")
    expect(s.minute).toBe("2025-07-15T08:45")
  })

  it("names no model where it prints none", async () => {
    expect((await sheet(without("MC-780"))).model).toBeUndefined()
  })

  it("stops where a label or its value is not where the layout puts it", async () => {
    for (const runs of [
      without("Weight", "F2"),
      without("10/02/2026 09:20"),
      without("kcal"),
      without("Trunk", "F4"),
      without("95"),
      without("6.2�"),
      without("kg", "F3"),
      // a second number beside a value
      [...SHEET_RUNS, [684, 140, "F3", "81.7"] as SheetRun],
      // a second segment label in one half
      [...SHEET_RUNS, [200, 100, "F4", "Left Arm"] as SheetRun],
    ]) {
      const e = await failure(runs)
      expect(e).toMatchObject({ _tag: "Unreadable", path: PATH })
      expect((e as { message: string }).message).toMatch(
        /^the body composition sheet is not in the layout this importer reads: /
      )
    }
  })

  it("takes the segment value nearest its label, and only a range on the value's line", async () => {
    const s = await sheet([
      ...SHEET_RUNS.filter((r) => r[3] !== "68.0-83.0"),
      [690, 190, "F3", "60.0-70.0"],
    ])
    expect(s.rows[0]).toMatchObject({ name: "Weight", value: "81.6" })
    expect(s.rows[0].low).toBeUndefined()
    expect(s.rows.find((r) => r.name === "Muscle Mass Right Arm")!.value).toBe(
      "3.5"
    )
  })

  it("stops where the PDF shows no text", async () => {
    const r = await runResult(readSheet(pdfFixture(["0 0 m 10 10 l S"]), PATH))
    expect(r._tag === "Failure" && r.failure).toMatchObject({
      _tag: "Unreadable",
      path: PATH,
    })
  })
})
