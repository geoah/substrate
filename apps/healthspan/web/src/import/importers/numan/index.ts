// Numan's JSON export, one or several, as an importer (apps/healthspan/docs/
// importers/01-numan.md): every test once, from the export holding its
// newest version, as a lab report of the lab that ran it (Randox or
// Hurdle), its results with the lab's range and Numan's clinical range, and
// the test's own JSON as the report's file. Every test is credited to the
// Numan source, whichever lab ran it.

import { Effect, Option } from "effect"
import { sourceRecord } from "../shared"
import type { Importer, LeftOut, Plan, PlannedDocument } from "../types"
import { candidates, newestTests, numanJson, readExports } from "./exports"
import { NAMESPACE, SOURCE, readTest } from "./report"

const count = (n: number, one: string, many: string) =>
  `${n.toLocaleString("en-GB")} ${n === 1 ? one : many}`

export const numan: Importer = {
  id: "numan",
  name: "Numan export",
  description:
    "The JSON export of your tests from Numan's web app, one or several: each export repeats the older tests, and each test is read from the export holding its newest version.",
  accepts: { kind: "files", extensions: [".json"] },

  detect: (input) =>
    Effect.gen(function* () {
      let exports = 0
      const tests = new Set<string>()
      for (const file of candidates(input.files)) {
        const found = yield* numanJson(file)
        if (Option.isNone(found)) continue
        exports++
        for (const t of found.value.data)
          tests.add(
            (t as { attributes: { short_id: string } }).attributes.short_id
          )
      }
      if (!exports) return undefined
      const of = count(tests.size, "test", "tests")
      return {
        importer: "numan" as const,
        confidence: exports === input.files.length ? 1 : 0.9,
        why:
          exports === 1
            ? `A Numan export of ${of}.`
            : `${count(exports, "Numan export", "Numan exports")} of ${of}.`,
      }
    }),

  questions: () => Effect.succeed([]),

  read: (input, ctx) =>
    Effect.gen(function* () {
      const found = yield* readExports(input.files)
      if (!found.exports.length)
        return yield* Effect.fail({
          _tag: "NotMine" as const,
          message: "None of these files is a Numan export.",
        })
      const records = [
        sourceRecord({ namespace: NAMESPACE, name: "Numan", class: "service" }),
      ]
      const documents: PlannedDocument[] = []
      const left: LeftOut[] = [...found.left]
      for (const chosen of newestTests(found.exports)) {
        const out = yield* readTest(chosen, ctx)
        records.push(...out.records)
        documents.push(...out.documents)
        left.push(...out.left)
      }
      const plan: Plan = {
        importer: "numan",
        name: input.name,
        source: SOURCE,
        records,
        documents,
        left,
      }
      return plan
    }),
}
