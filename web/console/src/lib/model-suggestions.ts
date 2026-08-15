/** The models a chosen provider PRICES, offered on the model field (#54.1).
 *
 * `model` is a free-text string and has to stay one: a provider serves models
 * its pricing rows have never heard of, a new one ships the week it ships, and
 * a declaration that closed the set would be wrong within a month. But typing
 * a model id from memory is how you get `gpt-4.1-mni` and a 404 at the first
 * completion, and the provider row already carries the list — its `pricing`
 * rows name every model the cost accounting knows.
 *
 * So: suggestions, not an enum. The form offers what the provider prices, free
 * text stays legal, and a stored value the list does not carry is untouched.
 *
 * WHY THIS IS A CONSOLE-SIDE SPECIAL CASE, and not a declaration marker: what
 * it needs to say is "this property's suggestions come from a repeated field
 * of the record a SIBLING reference names", which is a vocabulary feature
 * (a `suggestsFrom:` beside `refersTo:`'s replacement) and a real bit of
 * design. The pairing is hard-coded here, in one named place, until that
 * exists. It is deliberately narrow: it fires on a `model` string beside a
 * `provider` reference and nowhere else.
 */

import type { SubstrateRecord } from "@/lib/api/types"

/** The property naming the provider, and the one naming the model. */
export const PROVIDER_PROPERTY = "provider"
export const MODEL_PROPERTY = "model"

/** The model ids a provider row prices, in declared order, deduped.
 *
 * `pricing` is a repeated object of `{model, inputPer1M, outputPer1M}`. A row
 * with no model id is not a suggestion; a duplicate is the same suggestion
 * twice, and the runtime already treats the later one as the winner. */
export function pricedModels(provider: SubstrateRecord | undefined): string[] {
  const rows = provider?.properties?.pricing
  if (!Array.isArray(rows)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const row of rows) {
    if (typeof row !== "object" || row === null) continue
    const model = (row as Record<string, unknown>).model
    if (typeof model !== "string") continue
    const id = model.trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  return out
}
