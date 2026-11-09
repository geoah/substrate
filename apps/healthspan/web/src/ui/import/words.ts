// How an import's state, a run's progress and a run's end read on the
// page.

import type { ImportStatus } from "../../kinds"
import type { RunState } from "../../import/registry"
import type { Tone } from "../tone"

export const STATUS: Record<ImportStatus, { word: string; tone: Tone }> = {
  planned: { word: "Planned", tone: "neutral" },
  importing: { word: "Importing", tone: "info" },
  stopped: { word: "Stopped", tone: "warning" },
  imported: { word: "Imported", tone: "success" },
  undoing: { word: "Undoing", tone: "info" },
  undone: { word: "Undone", tone: "neutral" },
}

const clock = (ms: number) =>
  new Date(ms).toLocaleTimeString("en-GB", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })

/** What a run in this page is doing now, or how it ended. */
export function runWords(run: RunState): string | undefined {
  const p = run.progress
  if (run.running) {
    if (!p) return run.mode === "undo" ? "Starting the undo…" : "Starting…"
    switch (p.phase) {
      case "taking":
        return "Starting…"
      case "waiting":
        return `Another browser is running it. The undo waits until ${clock(p.until)}.`
      case "setup":
        return "Setting up the lab's source…"
      case "writing":
        return `Writing ${p.done.toLocaleString("en-GB")} of ${p.total.toLocaleString("en-GB")} results…`
      case "settling":
        return `Waiting until ${clock(p.until)} for any write still in flight, then deleting.`
      case "deleting":
        return `Deleting ${p.done.toLocaleString("en-GB")} of ${p.total.toLocaleString("en-GB")}…`
    }
  }
  const o = run.outcome
  if (!o) return undefined
  switch (o.state) {
    case "held":
      return `Another browser is running it until ${clock(o.until)}. Resume after that, or Undo, which waits for it.`
    case "lost":
      return `This browser stopped: ${o.why} Resume picks it up where it is.`
    case "failed":
      return o.words
    case "cancelled":
      return "Stopped in this tab."
    default:
      return undefined
  }
}

/** `12 of 40 results`, `40 results`. */
export function countWords(results: number, rows: number, done: boolean) {
  const n = (x: number) => x.toLocaleString("en-GB")
  if (done) return `${n(rows)} ${rows === 1 ? "result" : "results"}`
  return `${n(results)} of ${n(rows)} results`
}
