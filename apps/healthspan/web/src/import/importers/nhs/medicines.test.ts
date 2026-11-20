import { describe, expect, it } from "vitest"
import {
  acuteStatus,
  dispenseStatus,
  epsMatches,
  epsWord,
  vaccineGiven,
} from "./medicines"

// The old script's tested rules (test_importers.py, AcuteStatus): one GP
// acute issue, judged on 2026-10-28.
describe("an acute issue's status", () => {
  const now = "2026-10-28"

  it("is active while new and unfilled", () => {
    expect(acuteStatus("2026-10-27", now, [])).toBe("active")
  })

  it("is active while it waits at the pharmacy", () => {
    expect(acuteStatus("2026-10-20", now, ["with dispenser"])).toBe("active")
    expect(acuteStatus("2026-10-20", now, ["to be dispensed"])).toBe("active")
    expect(acuteStatus("2026-10-20", now, ["with dispenser - active"])).toBe(
      "active"
    )
  })

  it("is completed once dispensed", () => {
    expect(acuteStatus("2026-10-01", now, ["collected"])).toBe("completed")
    expect(acuteStatus("2026-10-01", now, ["dispensed"])).toBe("completed")
  })

  it("is stopped once expired or not dispensed", () => {
    expect(acuteStatus("2026-03-01", now, ["expired"])).toBe("stopped")
    expect(acuteStatus("2026-09-01", now, ["not dispensed"])).toBe("stopped")
  })

  it("is cancelled once cancelled", () => {
    expect(acuteStatus("2026-09-01", now, ["cancelled"])).toBe("cancelled")
  })

  it("is unknown past its validity with no evidence", () => {
    // six months and a day: no EPS item says what happened
    expect(acuteStatus("2026-04-27", now, [])).toBe("unknown")
    expect(acuteStatus("2026-04-29", now, [])).toBe("active")
    expect(acuteStatus(null, now, [])).toBe("unknown")
  })

  it("keeps a month's last day where the later month has none", () => {
    // 31 August and six months is 28 February
    expect(acuteStatus("2026-08-31", "2027-02-27", [])).toBe("active")
    expect(acuteStatus("2026-08-31", "2027-02-28", [])).toBe("unknown")
  })

  it("is completed for a vaccine given that day", () => {
    expect(acuteStatus("2021-05-20", now, [], true)).toBe("completed")
  })

  it("completes a vaccine only on its own administration", () => {
    const day = "2025-03-01"
    const record = [
      { date: "2025-03-01", term: "Typhoid vaccination" },
      { date: "2025-03-01", term: "Requires a hepatitis B vaccination" },
      {
        date: "2025-03-01",
        term: "Influenza vaccination invitation SMS (short message service) text message sent",
      },
    ]
    // the same antigen, given that day, by brand on one side and word on
    // the other
    expect(
      vaccineGiven("Typhim Vi 25micrograms/0.5ml vaccine", day, record)
    ).toBe(true)
    expect(
      vaccineGiven("Havrix Monodose vaccine", day, [
        {
          date: "2025-03-01",
          term: "Administration of adult hepatitis A vaccine",
        },
      ])
    ).toBe(true)
    // vaccine A does not complete because vaccine B was given that day
    expect(
      vaccineGiven("Avaxim vaccine suspension for injection", day, record)
    ).toBe(false)
    // "requires" and an invitation are no evidence of administration
    expect(
      vaccineGiven("Engerix B 20micrograms/1ml vaccine", day, record)
    ).toBe(false)
    expect(vaccineGiven("Fluad Tetra vaccine", day, record)).toBe(false)
    // the right vaccine on another day is no evidence either
    expect(vaccineGiven("Typhim Vi vaccine", "2025-03-02", record)).toBe(false)
    // a product with no antigen the table knows never matches
    expect(vaccineGiven("Exampleol 10mg tablets", day, record)).toBe(false)
    expect(vaccineGiven("Typhim Vi vaccine", null, record)).toBe(false)
  })

  it("falls back to dispensing and validity for an unmatched vaccine", () => {
    const record = [{ date: "2026-10-01", term: "Typhoid vaccination" }]
    const given = vaccineGiven("Avaxim vaccine", "2026-10-01", record)
    expect(acuteStatus("2026-10-01", now, [], given)).toBe("active")
    expect(acuteStatus("2025-10-01", now, [], false)).toBe("unknown")
    expect(acuteStatus("2026-10-01", now, ["collected"], given)).toBe(
      "completed"
    )
  })

  it("matches EPS items by name and a three-day window", () => {
    const eps = [
      {
        date: "2026-10-02",
        medications: [{ name: "Exampleol 10mg tablets", status: "Collected" }],
      },
      {
        date: "2026-06-01",
        medications: [{ name: "Exampleol 10mg tablets", status: "Expired" }],
      },
    ]
    expect(epsMatches("Exampleol 10mg tablets", "2026-10-01", eps)).toEqual([
      "collected",
    ])
    expect(epsMatches("Otherine 5mg", "2026-10-01", eps)).toEqual([])
    expect(epsMatches("Exampleol 10mg tablets", "2026-10-06", eps)).toEqual([])
    expect(epsMatches("Exampleol 10mg tablets", null, eps)).toEqual([])
    // a name matches by its first 25 characters, either way round
    expect(
      epsMatches("Exampleol 10mg tablets (Example Pharma Ltd)", "2026-10-02", [
        {
          date: "2026-10-02",
          medications: [
            { name: "Exampleol 10mg tablets", status: "Dispensed" },
          ],
        },
      ])
    ).toEqual(["dispensed"])
  })
})

describe("a pharmacy's word for an item", () => {
  it("reads the words the app now sends as one as the tracker's spaced words", () => {
    expect(epsWord("WithDispenser")).toBe("with dispenser")
    expect(epsWord("WithDispenserActive")).toBe("with dispenser - active")
    expect(epsWord("ToBeDispensed")).toBe("to be dispensed")
    expect(epsWord("NotDispensed")).toBe("not dispensed")
    expect(epsWord("NotTracked")).toBe("not tracked")
    expect(epsWord(" Collected ")).toBe("collected")
    expect(epsWord("With dispenser - Active")).toBe("with dispenser - active")
    expect(epsWord(null)).toBe("")
  })

  it("is a dispense's status, unknown for a word the tracker does not use", () => {
    expect(dispenseStatus("Collected")).toBe("completed")
    expect(dispenseStatus("Dispensed")).toBe("completed")
    expect(dispenseStatus("WithDispenser")).toBe("preparation")
    expect(dispenseStatus("WithDispenserActive")).toBe("inprogress")
    expect(dispenseStatus("ToBeDispensed")).toBe("preparation")
    expect(dispenseStatus("Cancelled")).toBe("cancelled")
    expect(dispenseStatus("Expired")).toBe("cancelled")
    expect(dispenseStatus("NotDispensed")).toBe("declined")
    expect(dispenseStatus("NotTracked")).toBe("unknown")
    expect(dispenseStatus("")).toBe("unknown")
  })

  it("decides an acute issue's status in its spaced form", () => {
    const eps = [
      {
        date: "2026-10-20",
        medications: [
          { name: "Exampleol 10mg tablets", status: "NotDispensed" },
        ],
      },
    ]
    expect(
      acuteStatus(
        "2026-10-20",
        "2026-10-28",
        epsMatches("Exampleol 10mg tablets", "2026-10-20", eps)
      )
    ).toBe("stopped")
  })
})
