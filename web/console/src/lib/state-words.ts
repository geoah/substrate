/** A stored state value in everyday words, and the colour of what it means.
 * The words are a console label only; the stored value is what a filter, a
 * write and technical mode use. */

export type StateTone = "active" | "ok" | "pending" | "stopped" | "bad"

const WORDS: Record<string, string> = {
  proposed: "Suggested",
  abandoned: "Dropped",
}

const TONES: Record<string, StateTone> = {
  open: "active",
  active: "active",
  running: "active",
  started: "active",
  inprogress: "active",
  done: "ok",
  known: "ok",
  accepted: "ok",
  approved: "ok",
  merged: "ok",
  completed: "ok",
  succeeded: "ok",
  connected: "ok",
  ok: "ok",
  proposed: "pending",
  pending: "pending",
  waiting: "pending",
  draft: "pending",
  queued: "pending",
  abandoned: "stopped",
  rejected: "stopped",
  cancelled: "stopped",
  canceled: "stopped",
  closed: "stopped",
  archived: "stopped",
  superseded: "stopped",
  utility: "stopped",
  failed: "bad",
  error: "bad",
  parked: "bad",
  refused: "bad",
}

export function stateWord(value: string): string {
  const word = WORDS[value]
  if (word) return word
  const spaced = value.replace(/[_-]+/g, " ")
  return spaced ? spaced[0].toUpperCase() + spaced.slice(1) : spaced
}

/** A value the table does not know reads as waiting while the machine sits
 * at its initial state and as moving once it has left it. */
export function stateTone(value: string, initial?: string): StateTone {
  const tone = TONES[value.toLowerCase().replace(/[_\s-]+/g, "")]
  if (tone) return tone
  return initial !== undefined && value === initial ? "stopped" : "active"
}
