/** Alerts (decision record 0148): one `core/alert` record per ongoing problem
 * the substrate found, such as a trigger whose deliveries keep parking. The
 * engine opens, updates and resolves them; the console shows the open ones
 * where the records they are about live, and counts them on Home. */

import { readReference, type SubstrateRecord } from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"
import { CORE_PACKAGE } from "@/lib/api/http"
import { AGENT_KIND, FUNCTION_KIND } from "@/lib/tools"

export const ALERT_KIND = `${CORE_PACKAGE}/alert`

export type AlertLevel = "info" | "warning" | "error"

export interface Alert {
  id: string
  key: string
  level: AlertLevel
  summary: string
  /** The most recent error, in full; empty when the alert carries none. */
  detail: string
  about: string[]
  firstSeenAt: string
  lastSeenAt: string
  count: number | undefined
  open: boolean
}

const LEVELS: readonly AlertLevel[] = ["info", "warning", "error"]

function text(v: unknown): string {
  return typeof v === "string" ? v : ""
}

/** A level outside the declared set reads as `error`, so an alert is never
 * shown as milder than it might be. */
export function readAlert(record: SubstrateRecord): Alert {
  const p = record.properties
  const level = LEVELS.find((l) => l === p.level) ?? "error"
  const about = Array.isArray(p.about)
    ? p.about.flatMap((v) => {
        const ref = readReference(v)
        return ref ? [ref.path] : []
      })
    : []
  return {
    id: record.id,
    key: text(p.key) || record.id,
    level,
    summary: text(p.summary) || text(p.title) || record.id,
    detail: text(p.detail),
    about,
    firstSeenAt: text(p.firstSeenAt),
    lastSeenAt: text(p.lastSeenAt),
    count: typeof p.count === "number" ? p.count : undefined,
    open: p.state === "open",
  }
}

/** The first line of an alert's detail: a traceback's last line is the
 * error, but the first is what a row has room for. */
export function detailLine(alert: Alert): string {
  return alert.detail.split("\n", 1)[0].trim()
}

/** Where an alert lives in the console: the page of the agent or the tool
 * it is about, the first one its `about` names. Undefined when it names
 * neither, and the alert's own record is the place. */
export type AlertPlace =
  | { to: "agent"; id: string; name: string }
  | { to: "tool"; authority: string; pkg: string; name: string }

export function alertPlace(alert: Alert): AlertPlace | undefined {
  for (const path of alert.about) {
    const parts = splitRecordPath(path)
    if (!parts) continue
    const segments = parts.id.split("/")
    if (segments.length !== 3) continue
    const [authority, pkg, name] = segments
    if (parts.kind === AGENT_KIND) return { to: "agent", id: parts.id, name }
    if (parts.kind === FUNCTION_KIND)
      return { to: "tool", authority, pkg, name }
  }
  return undefined
}

/** The record paths of a package's callables, the `about` targets of the
 * alerts a provider's page shows: each function and agent the package
 * declares, by the identity the catalog lists it under. */
export function callablePaths(
  functions: readonly string[],
  agents: readonly string[]
): string[] {
  return [
    ...functions.map((ref) => `${FUNCTION_KIND}/${ref}`),
    ...agents.map((ref) => `${AGENT_KIND}/${ref}`),
  ]
}
