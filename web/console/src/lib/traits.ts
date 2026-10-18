/** Whether a kind binds a trait, read off its declaration alone: a leaf
 * module, so the declaration readers (lib/definition.ts) can ask it too. */

import { CORE_PACKAGE } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"

/** A kind carries a trait when its reconciled declaration lists it, by
 * its full identity (every stored declaration names it in full, decision
 * 0101) or by the bare shipped spelling an older declaration kept. A bare
 * `trait` argument names the core trait of that name. A binding that picks a
 * variant (`observed(packed)`, `temporal(point: dueAt)`) carries the trait
 * the part before its parenthesis names. */
export function kindHasTrait(
  kind: KindInfo | undefined,
  trait: string
): boolean {
  const traits = (kind?.definition as { traits?: unknown } | undefined)?.traits
  if (!Array.isArray(traits)) return false
  const bare = trait.slice(trait.lastIndexOf("/") + 1)
  const full = trait.includes("/") ? trait : `${CORE_PACKAGE}/${trait}`
  return traits.some((t) => {
    if (typeof t !== "string") return false
    const named = t.replace(/\s*\(.*\)\s*$/, "")
    return named === full || named === bare
  })
}
