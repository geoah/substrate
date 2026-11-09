/** Approvals (decision record 0254): an agent's call of a function declaring
 * `approval: required` lands an llm/approval instead of running, and the
 * owner approves or declines it here. Approving is ONE CAS'd patch moving
 * the record to `approved`; the engine then runs exactly the stored call and
 * reports the outcome into the conversation, which resumes the agent.
 *
 * The arguments are MODEL-AUTHORED: they render as plain untrusted text, and
 * what the decision sends is the state alone, never anything derived from
 * them. */

import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { patchRecord, recordsQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { APPROVAL_KIND, CORE_PACKAGE, LLM_PACKAGE } from "@/lib/core-kinds"
import { readReference } from "./types"

/** The collection segment of the approval kind (decision 0033). */
export const APPROVAL_NAME = "approval"

export type ApprovalState =
  "pending" | "approved" | "denied" | "running" | "done" | "failed"

/** One argument of the held call, as the card lists it. */
export interface ApprovalArgument {
  name: string
  label: string
  value: string
}

export interface ApprovalView {
  id: string
  version: number
  state: ApprovalState
  tool: string
  /** The asking agent's identity, `<authority>/<package>/<name>`. */
  agent: string
  /** The function the call would run, by identity. */
  function: string
  /** The thread whose agent asked, when it is not the conversation. */
  requestedIn?: string
  arguments: ApprovalArgument[]
  error?: string
}

const STATES: readonly ApprovalState[] = [
  "pending",
  "approved",
  "denied",
  "running",
  "done",
  "failed",
]

/** An agent's name from its identity: `…/assistant/secretary` is
 * "Secretary". */
export function agentWord(identity: string): string {
  const name = identity.slice(identity.lastIndexOf("/") + 1)
  return name ? name.charAt(0).toUpperCase() + name.slice(1) : "The agent"
}

/** An argument's name as a person reads it: `replyAll` is "Reply all". */
export function argumentLabel(name: string): string {
  const words = name.replace(/([a-z0-9])([A-Z])/g, "$1 $2").toLowerCase()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** An argument's value as text: a list joined, a flag in words, anything
 * else as JSON. */
function argumentText(value: unknown): string {
  if (typeof value === "string") return value
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "number") return String(value)
  if (Array.isArray(value) && value.every((v) => typeof v === "string"))
    return value.join(", ")
  return JSON.stringify(value)
}

/** A reference value's referent id: the path past the referent's kind, so
 * an agent's or a function's id keeps the slashes of its own. */
function referentId(value: unknown, kind: string): string {
  const path = readReference(value)?.path ?? ""
  if (path.startsWith(`${kind}/`)) return path.slice(kind.length + 1)
  return path.slice(path.lastIndexOf("/") + 1)
}

export function approvalViewOf(record: SubstrateRecord): ApprovalView {
  const p = record.properties
  const state = STATES.includes(p.state as ApprovalState)
    ? (p.state as ApprovalState)
    : "pending"
  const args =
    typeof p.arguments === "object" && p.arguments !== null
      ? (p.arguments as Record<string, unknown>)
      : {}
  const requestedIn = p.requestedIn
    ? referentId(p.requestedIn, `${LLM_PACKAGE}/thread`)
    : ""
  return {
    id: record.id,
    version: record.version,
    state,
    tool: typeof p.tool === "string" ? p.tool : "",
    agent: referentId(p.agent, `${CORE_PACKAGE}/agent`),
    function: referentId(p.function, `${CORE_PACKAGE}/function`),
    ...(requestedIn ? { requestedIn } : {}),
    arguments: Object.entries(args)
      .filter(([, v]) => v !== null && v !== undefined && v !== "")
      .map(([name, v]) => ({
        name,
        label: argumentLabel(name),
        value: argumentText(v),
      })),
    ...(typeof p.error === "string" && p.error ? { error: p.error } : {}),
  }
}

/** Approve or decline one approval, against the version the owner read. */
export function decideApproval(
  approval: Pick<ApprovalView, "id" | "version">,
  approve: boolean
): Promise<SubstrateRecord> {
  return patchRecord(
    CORE_AUTHORITY,
    LLM_PACKAGE_NAME,
    APPROVAL_NAME,
    approval.id,
    {
      properties: { state: approve ? "approved" : "denied" },
      ifVersion: approval.version,
    }
  )
}

/** The approvals asked on one conversation and still waiting. */
export function pendingApprovalsQueryOptions(thread: string) {
  return recordsQueryOptions({
    kinds: [APPROVAL_KIND],
    filter: {
      referencing: {
        ref: `${LLM_PACKAGE}/thread/${thread}`,
        property: "thread",
      },
      properties: { state: { eq: "pending" } },
    },
    first: 20,
  })
}
