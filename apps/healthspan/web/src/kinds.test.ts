// @vitest-environment node
import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import { Import, Profile, type ImportDraft, type ProfileDraft } from "./kinds"

/** The top-level property names a package file declares, and each object
 * property's field names, read from the YAML's indentation (the files keep
 * the house layout: properties at four spaces, fields at eight). */
function declared(file: string): {
  properties: string[]
  fields: Record<string, string[]>
} {
  const text = readFileSync(
    new URL(`../../package/${file}`, import.meta.url),
    "utf8"
  )
  const properties: string[] = []
  const fields: Record<string, string[]> = {}
  let inProperties = false
  let current = ""
  let inFields = false
  for (const line of text.split("\n")) {
    if (/^ {2}properties:$/.test(line)) {
      inProperties = true
      continue
    }
    if (!inProperties) continue
    const prop = /^ {4}([a-zA-Z]+):$/.exec(line)
    if (prop) {
      current = prop[1]
      properties.push(current)
      inFields = false
      continue
    }
    if (/^ {6}fields:$/.test(line)) {
      inFields = true
      fields[current] = []
      continue
    }
    const field = /^ {8}([a-zA-Z]+):$/.exec(line)
    if (field && inFields) fields[current].push(field[1])
  }
  return { properties, fields }
}

const fullProfile: ProfileDraft = {
  name: "Healthspan profile",
  contact: "c1",
  birthDate: "1980-01-02",
  sex: "female",
  nightSources: ["whoop/a"],
  workoutSources: ["whoop/a"],
  startedAt: "2026-11-09T10:00:00Z",
  recordSeenAt: "2026-11-09T10:00:00Z",
}

const fullImport: ImportDraft = {
  name: "results.csv",
  format: "csv",
  status: "importing",
  runner: { device: "d", until: "2026-11-09T10:01:00Z" },
  lab: "Example Lab",
  source: "lab/example",
  rows: [
    {
      key: "1",
      report: "r1",
      name: "LDL",
      metric: "labs/ldl_cholesterol",
      takenAt: "2026-11-01T08:00:00Z",
      value: "2.6",
      comparator: "<",
      unit: "mmol/L",
      text: "x",
      low: "0",
      high: "3",
      flag: "H",
    },
  ],
  reports: [{ key: "r1", takenAt: "2026-11-01T08:00:00Z", accession: "A1" }],
  results: 0,
  problem: null,
  importedAt: null,
  skipped: null,
}

describe("the package's kinds", () => {
  it("writes only what profile.yaml declares, and every one of it", () => {
    const { properties } = declared("profile.yaml")
    expect(Object.keys(Profile.write(fullProfile)).sort()).toEqual(
      [...properties].sort()
    )
  })

  it("writes only what import.yaml declares, and every one of it", () => {
    const { properties, fields } = declared("import.yaml")
    const written = Import.write(fullImport)
    expect(Object.keys(written).sort()).toEqual([...properties].sort())
    const row = (written.rows as Record<string, unknown>[])[0]
    expect(Object.keys(row).sort()).toEqual([...fields.rows].sort())
    const report = (written.reports as Record<string, unknown>[])[0]
    expect(Object.keys(report).sort()).toEqual([...fields.reports].sort())
    expect(Object.keys(written.runner as object).sort()).toEqual(
      [...fields.runner].sort()
    )
  })

  it("reads back what it writes", () => {
    const record = (kind: string, properties: Record<string, unknown>) => ({
      id: "x",
      kind,
      version: 3,
      properties,
      createdAt: "",
      updatedAt: "",
    })
    const p = Profile.read(record(Profile.kind, Profile.write(fullProfile)))
    expect(p).toMatchObject({
      contact: "c1",
      sex: "female",
      nightSources: ["whoop/a"],
      version: 3,
    })
    const i = Import.read(record(Import.kind, Import.write(fullImport)))
    expect(i.rows[0]).toEqual(fullImport.rows[0])
    expect(i.reports).toEqual(fullImport.reports)
    expect(i.source).toBe("lab/example")
    expect(i.status).toBe("importing")
  })
})
