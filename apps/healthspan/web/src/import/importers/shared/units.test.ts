import { describe, expect, it } from "vitest"
import { entryByCode } from "../../../catalogue"
import { sharedMetrics, sharedUnits } from "../testing"
import { analyteKey } from "./metric"
import {
  fixedFactor,
  knownUnit,
  ucum,
  UCUM_CODES,
  UNIT_OVERRIDES,
} from "./units"

describe("a printed unit", () => {
  it("is the table's UCUM code, else as printed", () => {
    expect(ucum("G_L")).toBe("g/L")
    expect(ucum("10⁹/L")).toBe("10*9/L")
    expect(ucum("\u00b5mol/l")).toBe("umol/L")
    expect(ucum("\u03bcmol/l")).toBe("umol/L")
    expect(ucum("ml/min/1.73m²")).toBe("mL/min/{1.73_m2}")
    expect(ucum("mg/mmol creatinine")).toBe("mg/mmol")
    expect(ucum("furlongs/L")).toBe("furlongs/L")
    expect(ucum(undefined)).toBeUndefined()
  })

  it("is what one analyte means by it where the table's overrides say", () => {
    expect(ucum("U/l", "fsh")).toBe("[IU]/L")
    expect(ucum("U/l")).toBe("U/L")
    expect(ucum("RATIO", "free_androgen_index")).toBe("%")
    expect(ucum("-", "fib4")).toBe("{score}")
    expect(ucum("-")).toBe("1")
    expect(ucum(undefined, "cholesterol_hdl_ratio")).toBe("1")
    expect(ucum("", "cholesterol_hdl_ratio")).toBe("1")
    expect(ucum("mmol/L", "blood_glucose")).toBe("mmol/L{glucose}")
    expect(ucum("ng/mL FEU", "d_dimer")).toBe("ng/mL")
    expect(UCUM_CODES.has("mmol/L{glucose}")).toBe(true)
  })

  it("reads the GP record's `.` for nucleated red cells per 100 white cells as a percent", () => {
    // The analyte as the NHS importer names it: a shared metric with no
    // catalogue entry, keyed by its id after the package.
    const analyte = analyteKey({
      metric: "labs/nucleated_red_cells_per_100_wbc",
    })
    expect(analyte).toBe("nucleated_red_cells_per_100_wbc")
    expect(ucum(".", analyte)).toBe("%")
    expect(knownUnit(".", analyte, sharedUnits())).toEqual({
      ucum: "%",
      id: "percent",
    })
    // Any other analyte's `.` is the table's: a ratio.
    expect(ucum(".")).toBe("1")
  })

  it("names by every override an analyte the importers can name", () => {
    // A catalogue entry's code, or a shared metric's id after the package:
    // an override keyed otherwise never applies.
    const metrics = new Set(
      sharedMetrics().map((m) => m.id.slice(m.id.indexOf("/") + 1))
    )
    const unknown = UNIT_OVERRIDES.map((o) => o.analyte).filter(
      (a) => !entryByCode(a) && !metrics.has(a)
    )
    expect(unknown).toEqual([])
  })

  it("is known where the code is the table's and the graph holds it", () => {
    const g = sharedUnits()
    expect(knownUnit("10⁹/L", undefined, g)).toEqual({
      ucum: "10*9/L",
      id: "1e9/L",
    })
    expect(knownUnit("PERCENT", undefined, g)).toEqual({
      ucum: "%",
      id: "percent",
    })
    expect(knownUnit("mmol/L", "blood_glucose", g)?.id).toBe("mmol/L_glucose")
    expect(knownUnit("U/L", "fsh", g)?.id).toBe("IU/L")
    expect(knownUnit("furlongs/L", undefined, g)).toBeUndefined()
    // In the graph, but not a spelling the table writes: kept as received.
    expect(knownUnit("mol/L", undefined, g)).toBeUndefined()
  })

  it("moves a provider's range only by a fixed factor", () => {
    expect(fixedFactor("g/dL", "g/L")).toBe("10")
    expect(fixedFactor("pg/mL", "ng/L")).toBe("1")
    expect(fixedFactor("ng/L", "pg/mL")).toBe("1")
    expect(fixedFactor("u[IU]/mL", "m[IU]/L")).toBe("1")
    expect(fixedFactor("g/L", "g/L")).toBe("1")
    expect(fixedFactor("nmol/L", "ug/L")).toBeUndefined()
    expect(fixedFactor("g/L", "g/dL")).toBeUndefined()
  })
})
