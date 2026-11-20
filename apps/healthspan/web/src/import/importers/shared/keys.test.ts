import { createHash } from "node:crypto"
import { describe, expect, it } from "vitest"
import {
  docRef,
  keyHash,
  KINDS,
  metricPath,
  numberedKeys,
  organizationKey,
  ref,
  slug,
  sourceKey,
} from "./keys"

describe("a key's hash", () => {
  it("is SHA-256 over the parts as JSON text, joined by U+001F", () => {
    const want = createHash("sha256")
      .update(
        '"2026-01-01T09:30:00+00:00"\u001f"EXAMPLE, Chris (Dr)"\u001fnull\u001f3'
      )
      .digest("hex")
    expect(
      keyHash("2026-01-01T09:30:00+00:00", "EXAMPLE, Chris (Dr)", null, 3)
    ).toBe(want)
    expect(keyHash("x", undefined)).toBe(keyHash("x", null))
    expect(keyHash("a", "b")).not.toBe(keyHash("a\u001fb"))
  })

  it("numbers a key's later repeats in order", () => {
    expect(numberedKeys(["a", "b", "a", "a"])).toEqual(["a", "b", "a-2", "a-3"])
  })
})

describe("a slug", () => {
  it("is lower case, a percent as pct, every other run one hyphen", () => {
    const cases: [string, string][] = [
      ["reg_tissue_fat/left_arm", "reg-tissue-fat-left-arm"],
      ["bmd_t_score", "bmd-t-score"],
      ["Total mass", "total-mass"],
      ["Region %Fat (total)", "region-pct-fat-total"],
      ["A/G ratio", "a-g-ratio"],
      ["Centile (US reference, v107)", "centile-us-reference-v107"],
      ["25-OHVitaminD", "25-ohvitamind"],
      ["Phase Angle H-L (50 kHz)", "phase-angle-h-l-50-khz"],
      ["Fat % Left Arm", "fat-pct-left-arm"],
      ["TBW %", "tbw-pct"],
      ["Microalbumin / Creatinine Ratio", "microalbumin-creatinine-ratio"],
      ["Σύνολο 1.5", "1.5"],
    ]
    for (const [text, s] of cases) expect(slug(text)).toBe(s)
  })
})

describe("keys and references", () => {
  it("names a provider's source and an organization by their shared keys", () => {
    expect(sourceKey("dxa/centre")).toEqual({
      kind: KINDS.source,
      namespace: "source",
      key: "dxa/centre",
    })
    expect(organizationKey("A12345")).toEqual({
      kind: "substrate.reamde.dev/clinical/organization",
      namespace: "ods",
      key: "A12345",
    })
  })

  it("refers to a record by its key and a document by its digest", () => {
    const key = sourceKey("numan")
    expect(ref(key)).toEqual({ $ref: key })
    expect(ref(key).$ref).not.toBe(key)
    expect(docRef("blob-sha256-ab")).toEqual({ $doc: "blob-sha256-ab" })
    expect(metricPath("labs/ldl_cholesterol")).toBe(
      "substrate.reamde.dev/observations/metric/labs/ldl_cholesterol"
    )
  })
})
