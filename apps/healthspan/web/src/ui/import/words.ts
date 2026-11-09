// How an import's state, its importer, a run's progress, a run's end and a
// run's counts read on the page.

import type { ImportCounts, ImportStatus, ImporterId } from "../../kinds"
import type { RunState } from "../../import/registry"
import type { Tone } from "../tone"

export const STATUS: Record<ImportStatus, { word: string; tone: Tone }> = {
  staging: { word: "Staging", tone: "info" },
  planned: { word: "Planned", tone: "neutral" },
  importing: { word: "Importing", tone: "info" },
  stopped: { word: "Stopped", tone: "warning" },
  imported: { word: "Imported", tone: "success" },
  undoing: { word: "Undoing", tone: "info" },
  undone: { word: "Undone", tone: "neutral" },
}

/** What each importer is called on Sources and an import's page, without
 * loading it. */
export const IMPORTER_WORDS: Record<ImporterId, string> = {
  csv: "From a CSV file",
  manual: "Entered by hand",
  numan: "From a Numan export",
  randox: "From Randox",
  dxa: "From DXA scans",
  nhs: "From the NHS App",
  mychart: "From MyChart",
}

const KIND_WORDS: Record<string, [string, string]> = {
  labreport: ["lab report", "lab reports"],
  labresult: ["lab result", "lab results"],
  source: ["source", "sources"],
  organization: ["care provider", "care providers"],
  patient: ["patient registration", "patient registrations"],
  encounter: ["consultation", "consultations"],
  condition: ["condition", "conditions"],
  allergy: ["allergy", "allergies"],
  immunization: ["immunisation", "immunisations"],
  medicationrequest: ["medicine", "medicines"],
  medicationdispense: ["dispensing", "dispensings"],
  appointment: ["appointment", "appointments"],
  referral: ["referral", "referrals"],
  document: ["document", "documents"],
  communication: ["message", "messages"],
}

const n = (x: number) => x.toLocaleString("en-GB")

/** `12 lab results`: a kind's records counted, in words. */
export function kindCount(kind: string, count: number): string {
  const name = kind.slice(kind.lastIndexOf("/") + 1)
  const [one, many] = KIND_WORDS[name] ?? [name, `${name} records`]
  return `${n(count)} ${count === 1 ? one : many}`
}

/** A kind's name for a heading: `Lab results`. */
export function kindHeading(kind: string): string {
  const name = kind.slice(kind.lastIndexOf("/") + 1)
  const many = KIND_WORDS[name]?.[1] ?? name
  return many.charAt(0).toUpperCase() + many.slice(1)
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
    if (!p)
      return run.mode === "undo"
        ? "Starting the undo…"
        : run.mode === "discard"
          ? "Discarding…"
          : "Starting…"
    switch (p.phase) {
      case "taking":
        return "Starting…"
      case "waiting":
        return run.mode === "discard"
          ? `Another browser is staging it. The discard waits until ${clock(p.until)}.`
          : `Another browser is running it. The undo waits until ${clock(p.until)}.`
      case "staging":
        return p.done < p.total
          ? `Sending ${n(p.done + 1)} of ${n(p.total)} documents…`
          : "Sending its plan…"
      case "setup":
        return "Setting up its source…"
      case "writing":
        return `Writing ${n(p.done)} of ${n(p.total)} records…`
      case "settling":
        return `Waiting until ${clock(p.until)} for any write still in flight, then deleting.`
      case "deleting":
        return `Deleting ${n(p.done)} of ${n(p.total)}…`
      case "discarding":
        return "Discarding…"
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
    case "discarded":
      return "Discarded."
    case "nothing":
      return o.status === "staging"
        ? "It is still being staged by the browser that read its files. Finish it there, or discard it."
        : undefined
    default:
      return undefined
  }
}

/** A run's counts in words, the zeros left out: `12 created, 3 updated`. */
export function countsWords(c: ImportCounts | undefined): string | undefined {
  if (!c) return undefined
  const parts = [
    c.created ? `${n(c.created)} created` : "",
    c.updated ? `${n(c.updated)} updated` : "",
    c.unchanged ? `${n(c.unchanged)} unchanged` : "",
    c.stale ? `${n(c.stale)} older than what is there` : "",
    c.left
      ? `${n(c.left)} ${c.left === 1 ? "reference" : "references"} left out`
      : "",
  ].filter(Boolean)
  return parts.length ? parts.join(", ") : "Nothing written"
}

/** How many records point at an import, in words. */
export function createdWords(created: number): string {
  return `${n(created)} ${created === 1 ? "record" : "records"}`
}
