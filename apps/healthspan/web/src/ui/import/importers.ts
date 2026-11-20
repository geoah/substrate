// The importers the import screen offers (docs/plans/healthspan.md,
// Importers): the file of lab results and the hand entry, which load with
// the screen, and every provider's, each loaded when the screen asks what
// an input is, so a provider's parsers (and a PDF reader) load only then.
// Effect stays behind this: the screen sees promises.

import { Effect, Result } from "effect"
import { IMPORTERS } from "../../import/importers"
import { csv } from "../../import/importers/csv"
import { manual } from "../../import/importers/manual"
import type {
  Context,
  ImportError,
  Importer,
  ImporterId,
  Input,
  Match,
  Plan,
  Question,
} from "../../import/importers/types"

export { csv, manual }

/** Every importer that reads files: the CSV first, then the providers in
 * the order the plan lists them. One that fails to load is left out. */
export async function fileImporters(): Promise<Importer[]> {
  const ids = Object.keys(IMPORTERS) as (keyof typeof IMPORTERS)[]
  const loaded = await Promise.all(
    ids.map((id) => IMPORTERS[id]().catch(() => undefined))
  )
  return [csv, ...loaded.filter((i): i is Importer => !!i)]
}

export interface Detected {
  importer: Importer
  match: Match
}

/** The importers that take an input as their own, the surest first. */
export async function detectAll(
  input: Input,
  importers: readonly Importer[],
  signal?: AbortSignal
): Promise<Detected[]> {
  const found = await Promise.all(
    importers.map(async (importer) => {
      try {
        const match = await Effect.runPromise(importer.detect(input), {
          signal,
        })
        return match ? { importer, match } : undefined
      } catch {
        // An importer that cannot tell is not a match.
        return undefined
      }
    })
  )
  return found
    .filter((d): d is Detected => !!d)
    .sort((a, b) => b.match.confidence - a.match.confidence)
}

/** The questions an importer asks of an input, its suggestions filled. */
export async function questionsOf(
  importer: Importer,
  input: Input,
  ctx: Context,
  signal?: AbortSignal
): Promise<readonly Question[]> {
  return Effect.runPromise(importer.questions(input, ctx), { signal })
}

/** An importer's reading: its plan, or why not. */
export async function readPlan(
  importer: Importer,
  input: Input,
  ctx: Context,
  signal?: AbortSignal
): Promise<{ plan: Plan } | { error: ImportError }> {
  const out = await Effect.runPromise(
    Effect.result(importer.read(input, ctx)),
    { signal }
  )
  return Result.isSuccess(out) ? { plan: out.success } : { error: out.failure }
}

/** An importer's error in words. */
export function errorWords(e: ImportError): string {
  switch (e._tag) {
    case "Unreadable": {
      const where = [e.path, e.field].filter(Boolean).join(", ")
      return where ? `${e.message} (${where})` : e.message
    }
    case "NotMine":
      return e.message
    case "NeedsAnswers":
      return `It needs ${e.questions.map((q) => q.label.toLowerCase()).join(" and ")}.`
  }
}

export type { ImporterId }
