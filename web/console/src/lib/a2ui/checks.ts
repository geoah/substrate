/** A Checkable component's `checks`: each a condition that answers a
 * boolean or a validation result `{valid, message}`. A failing check says
 * its message under the input, and a button with one stays disabled. */

import { isRecord } from "./coerce"
import { resolve, type EvalContext } from "./values"

const FALLBACK = "This value isn’t valid."

/** The messages of the checks that fail, in order; empty when all pass. */
export function failingChecks(checks: unknown, ctx: EvalContext): string[] {
  if (!Array.isArray(checks)) return []
  const out: string[] = []
  for (const rule of checks) {
    if (!isRecord(rule)) continue
    const result = resolve(rule.condition, ctx)
    const valid = isRecord(result) ? result.valid === true : result === true
    if (valid) continue
    const own = isRecord(result) ? result.message : undefined
    out.push(
      typeof own === "string" && own
        ? own
        : typeof rule.message === "string" && rule.message
          ? rule.message
          : FALLBACK
    )
  }
  return out
}
