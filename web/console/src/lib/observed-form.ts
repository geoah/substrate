/** Core's `observed` trait and the words the console reads its kinds'
 * `form` values in (decision 0156): one set on every surface, the
 * observations pages, a record's sheet and its forms, and a collection's grid
 * and filters. A leaf module, so the declaration readers can use it. */

import type { EnumValue, KindInfo } from "@/lib/api/types"
import { OBSERVED_TRAIT } from "@/lib/core-kinds"
import { kindHasTrait } from "@/lib/traits"

/** The everyday words for each stored form. */
export const FORM_WORDS = {
  gauge: "Level",
  amount: "Amount",
  counter: "Running total",
  summary: "Vendor summary",
} as const

/** A declared enum's values, the `form` of a kind binding the trait labelled
 * in `FORM_WORDS` where the declaration authors no label of its own. */
export function withFormWords(
  kind: KindInfo | undefined,
  name: string,
  values: EnumValue[] | undefined
): EnumValue[] | undefined {
  if (name !== "form" || !values || !kindHasTrait(kind, OBSERVED_TRAIT))
    return values
  return values.map((v) => {
    const word = FORM_WORDS[v.value as keyof typeof FORM_WORDS]
    return v.label || !word ? v : { ...v, label: word }
  })
}
