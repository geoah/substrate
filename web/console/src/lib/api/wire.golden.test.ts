/** The console's half of the wire-drift guard.
 *
 * `types.ts` is written BY HAND to mirror the Go structs in
 * `internal/substrate`. Nothing generates it, so the two can drift silently —
 * a renamed Go field is invisible here until something breaks in a browser.
 *
 * `wire.golden.json` is what stops that. A Go test
 * (`internal/substrate/wire_test.go`) reflects over the structs and writes the
 * field names it serializes into that file; this test asserts the TypeScript
 * interfaces carry exactly those keys.
 *
 * The trick that makes it real rather than decorative is the `Record<keyof T,
 * true>` maps below. TypeScript types are erased at runtime, so a test cannot
 * enumerate an interface's keys — but a `Record<keyof T, true>` literal will
 * not COMPILE unless it lists every key of `T` and no others. So each map is
 * checked twice: by `tsc`, against the interface, and here, against the
 * golden. A field that moves in Go fails the Go test; once the golden is
 * regenerated, it fails here until the interface and its map agree.
 *
 * When this fails, fix `types.ts` — the golden is the server's word, not a
 * suggestion. */

import { describe, expect, it } from "vitest"

import golden from "./wire.golden.json"
import type {
  BundleClosure,
  CatalogBundle,
  Change,
  IncomingReference,
  IncomingSource,
  Occurrence,
  OccurrenceList,
  OccurrenceLog,
  OccurrenceProblem,
  OperationalList,
  PropertyAlternative,
  PropertyMeta,
  PutInput,
  ShippedRecord,
  SubstrateRecord,
  SuggestedMapping,
} from "./types"

/** Every key of T, exactly once. `tsc` rejects a missing or an extra one. */
type Keys<T> = Record<keyof T, true>

const substrateRecord: Keys<SubstrateRecord> = {
  id: true,
  kind: true,
  canonicalId: true,
  formerIds: true,
  properties: true,
  labels: true,
  annotations: true,
  version: true,
  createdAt: true,
  updatedAt: true,
  deletedAt: true,
  finalizers: true,
  propertyMeta: true,
}

const incomingReference: Keys<IncomingReference> = {
  property: true,
  path: true,
  from: true,
}

const incomingSource: Keys<IncomingSource> = {
  id: true,
  kind: true,
  title: true,
}

const propertyMeta: Keys<PropertyMeta> = {
  manager: true,
  tier: true,
  updatedAt: true,
  alternatives: true,
}

const propertyAlternative: Keys<PropertyAlternative> = {
  actor: true,
  value: true,
  updatedAt: true,
}

/** PutInput carries `kind` for the editor's benefit even though the REST body
 * omits it (the collection path already said the kind), which is why it is
 * here rather than being treated as an extra key. */
const putInput: Keys<PutInput> = {
  kind: true,
  id: true,
  properties: true,
  labels: true,
  annotations: true,
  ifVersion: true,
}

/** ChangeRow extends this with `triggers`, which is the feed's decoration,
 * not the entry: the golden holds the entry alone. */
const change: Keys<Change> = {
  seq: true,
  ts: true,
  actor: true,
  op: true,
  recordId: true,
  kind: true,
  payload: true,
  hash: true,
}

/** The operational-list envelope is generic; its keys do not depend on the
 * element, so `unknown` pins them. */
const operationalList: Keys<OperationalList<unknown>> = {
  items: true,
  cursor: true,
}

const occurrence: Keys<Occurrence> = {
  kind: true,
  id: true,
  title: true,
  at: true,
  log: true,
}

const occurrenceLog: Keys<OccurrenceLog> = {
  kind: true,
  id: true,
  status: true,
}

const occurrenceProblem: Keys<OccurrenceProblem> = {
  kind: true,
  id: true,
  message: true,
}

const occurrenceList: Keys<OccurrenceList> = {
  occurrences: true,
  truncated: true,
  problems: true,
}

/** The catalog entry, and the two shapes nested in it. `installed` and
 * `upgrade` are NOT here: the API adds them around the bundle, and the
 * console carries them on CatalogItem. */
const catalogBundle: Keys<CatalogBundle> = {
  id: true,
  name: true,
  authority: true,
  package: true,
  description: true,
  version: true,
  tier: true,
  inputs: true,
  requires: true,
  suggestedMappings: true,
  closure: true,
}

const suggestedMapping: Keys<SuggestedMapping> = {
  id: true,
  from: true,
  to: true,
  package: true,
  state: true,
}

const bundleClosure: Keys<BundleClosure> = {
  kinds: true,
  kindDescriptions: true,
  functions: true,
  agents: true,
  mappings: true,
  records: true,
}

const shippedRecord: Keys<ShippedRecord> = {
  kind: true,
  id: true,
}

const mirrors: Record<string, Record<string, true>> = {
  SubstrateRecord: substrateRecord,
  IncomingReference: incomingReference,
  IncomingSource: incomingSource,
  PropertyMeta: propertyMeta,
  PropertyAlternative: propertyAlternative,
  PutInput: putInput,
  Change: change,
  OperationalList: operationalList,
  Occurrence: occurrence,
  OccurrenceLog: occurrenceLog,
  OccurrenceProblem: occurrenceProblem,
  OccurrenceList: occurrenceList,
  CatalogBundle: catalogBundle,
  BundleClosure: bundleClosure,
  ShippedRecord: shippedRecord,
  SuggestedMapping: suggestedMapping,
}

describe("wire types mirror the Go structs", () => {
  it("covers every shape the golden names", () => {
    // A shape added on the Go side must be mirrored here, not quietly skipped.
    expect(Object.keys(mirrors).sort()).toEqual(Object.keys(golden).sort())
  })

  for (const [name, fields] of Object.entries(
    golden as Record<string, string[]>
  )) {
    it(`${name} has exactly the server's fields`, () => {
      const mirror = mirrors[name]
      expect(mirror, `${name} is in the golden but not mirrored`).toBeDefined()
      // Sorted: declaration order is Go's business, not the wire's.
      expect(Object.keys(mirror).sort()).toEqual([...fields].sort())
    })
  }
})
