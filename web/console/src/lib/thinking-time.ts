/** The words the console puts on how long a model thought. */

import type { TurnView } from "@/lib/api/transcript"

/** A duration as a reader says it: whole seconds, then minutes and seconds.
 * Anything under a second reads as one second. */
export function thinkingDuration(ms: number): string {
  const seconds = Math.max(1, Math.round(ms / 1000))
  if (seconds < 60) return `${seconds} s`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return rest ? `${minutes} min ${rest} s` : `${minutes} min`
}

/** The line's words: "Thinking for 3 s" counting up while the turn is
 * live, "Thought for 12 s" once it settled, and "Thought" for a turn whose
 * time was not measured (a completion that streamed nothing). */
export function thinkingLabel(
  turn: Pick<TurnView, "reasoningMillis" | "reasoningStartedAt">,
  now: number
): string {
  if (turn.reasoningStartedAt !== undefined) {
    return `Thinking for ${thinkingDuration(now - turn.reasoningStartedAt)}`
  }
  if (turn.reasoningMillis !== undefined) {
    return `Thought for ${thinkingDuration(turn.reasoningMillis)}`
  }
  return "Thought"
}
