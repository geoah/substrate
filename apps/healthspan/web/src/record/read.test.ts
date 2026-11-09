import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { providerOf, providersOf, toRecord, type Provenance } from "./read"

const record = (
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id,
  kind,
  properties,
  version: 1,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
})

const C = "substrate.reamde.dev/clinical"
const IMPORT = "substrate.reamde.dev/healthspan/import"

const prov: Provenance = {
  importSources: new Map([
    ["imp-1", "src-gp"],
    ["imp-2", "src-gp"],
    ["imp-3", "src-hospital"],
  ]),
  sourceNames: new Map([
    ["src-gp", "A GP practice"],
    ["src-hospital", "A hospital"],
  ]),
}

describe("a clinical row's provider", () => {
  it("is the source of the import that created it, whatever its minted id", () => {
    expect(
      providerOf(
        record(`${C}/condition`, "01abc", {
          origin: { ref: `${IMPORT}/imp-1` },
        }),
        prov
      )
    ).toBe("source:src-gp")
  })

  it("is the id's first segment for a provider that keeps its upstream's ids", () => {
    expect(
      providerOf(record(`${C}/condition`, "nhs/owner/problem/x", {}), prov)
    ).toBe("nhs")
  })

  it("groups two imports of one source as one provider, named by the source", () => {
    const rec = toRecord(
      {
        condition: [
          record(`${C}/condition`, "01a", {
            name: "One",
            origin: { ref: `${IMPORT}/imp-1` },
          }),
          record(`${C}/condition`, "01b", {
            name: "Two",
            origin: { ref: `${IMPORT}/imp-2` },
          }),
        ],
        communication: [
          record(`${C}/communication`, "01c", {
            subject: "A letter",
            origin: { ref: `${IMPORT}/imp-3` },
          }),
        ],
      },
      prov
    )
    expect(providersOf(rec)).toEqual([
      { id: "source:src-gp", name: "A GP practice" },
      { id: "source:src-hospital", name: "A hospital" },
    ])
  })
})
