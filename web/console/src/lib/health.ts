/** Trigger health (#879): a trigger reads `failing` while its
 * `trigger.failing/<id>` alert is open, which the server opens once every
 * delivery since the trigger's newest ok run has failed for longer than its
 * window, and resolves on the next ok delivery. The agent, tool and provider
 * pages show it for the triggers that run their callables. */

import type { TriggerStatus } from "@/lib/api/types"

/** The failing triggers of a set of callables: their ids, and the oldest
 * failingSince among them (undefined when none carries one). */
export interface FailingTriggers {
  triggers: string[]
  since: string | undefined
}

/** The triggers among `statuses` that run one of `callables` (by identity,
 * as a status names its callable) and read `failing`. Undefined when none
 * does, so a healthy page renders nothing. */
export function failingTriggersOf(
  statuses: readonly TriggerStatus[],
  callables: readonly string[]
): FailingTriggers | undefined {
  const wanted = new Set(callables)
  const failing = statuses.filter(
    (s) => s.health === "failing" && wanted.has(s.callable)
  )
  if (!failing.length) return undefined
  const since = failing
    .map((s) => s.failingSince)
    .filter((at): at is string => Boolean(at))
    .sort((a, b) => Date.parse(a) - Date.parse(b))[0]
  return { triggers: failing.map((s) => s.id).sort(), since }
}
