// @vitest-environment node
import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import {
  IMPORTER_IDS,
  Import,
  Profile,
  type ImportDraft,
  type ImportStatus,
  type ProfileDraft,
} from "./kinds"

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

/** The items of a list a property declares (`values:`, `states:`), in
 * order. */
function listed(file: string, prop: string, key: string): string[] {
  const lines = readFileSync(
    new URL(`../../package/${file}`, import.meta.url),
    "utf8"
  ).split("\n")
  const at = lines.indexOf(`    ${prop}:`)
  const start = lines.indexOf(`      ${key}:`, at)
  const out: string[] = []
  for (const line of lines.slice(start + 1)) {
    const m = /^ {8}- (\S+)$/.exec(line)
    if (!m) break
    out.push(m[1])
  }
  return out
}

const DIGEST = `blob-sha256-${"a".repeat(64)}`

const fullImport: ImportDraft = {
  name: "results.csv",
  importer: "csv",
  status: "importing",
  runner: { device: "d", until: "2026-11-09T10:01:00Z" },
  lab: "Example Lab",
  source: "lab/example",
  plan: DIGEST,
  documents: [`blob-sha256-${"b".repeat(64)}`],
  counts: { created: 3, updated: 2, unchanged: 1, stale: 1, left: 1 },
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
    expect(Object.keys(written.runner as object).sort()).toEqual(
      [...fields.runner].sort()
    )
    expect(Object.keys(written.counts as object).sort()).toEqual(
      [...fields.counts].sort()
    )
  })

  it("names import.yaml's importers and states, in its order", () => {
    expect(listed("import.yaml", "importer", "values")).toEqual(IMPORTER_IDS)
    const states: ImportStatus[] = [
      "staging",
      "planned",
      "importing",
      "stopped",
      "imported",
      "undoing",
      "undone",
    ]
    expect(listed("import.yaml", "status", "states")).toEqual(states)
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
    expect(i).toMatchObject({
      importer: "csv",
      status: "importing",
      source: "lab/example",
      plan: DIGEST,
      documents: fullImport.documents,
      counts: fullImport.counts,
      runner: fullImport.runner,
    })
    // A blob reference as served carries its manifest beside the digest.
    const served = Import.read(
      record(Import.kind, {
        ...Import.write(fullImport),
        plan: { digest: DIGEST, size: 10, status: "stored" },
        documents: [{ digest: fullImport.documents[0] }],
      })
    )
    expect(served.plan).toBe(DIGEST)
    expect(served.documents).toEqual(fullImport.documents)
  })
})
