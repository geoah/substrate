/** "Always allow this": the standing rules an owner writes from a suggested
 * change that a gate held, and the words they read in.
 *
 * An `allow` recordpatchpolicy outranks a gate only by naming it in
 * `overrides` (decision 0109), and the write door admits `overrides` only on
 * an allow whose selector names exactly one agent, one kind reference and one
 * op. A gated create or patch request does not record whether the agent
 * called `put` or `patch` (policy.go convertToRequest maps both onto the
 * request's op by whether the target exists), so one click writes one allow
 * per verb: `put` and `patch` for a create or a patch, `delete` for a delete.
 * The pair is one rule to the person, listed and revoked together. */

import { lowerFirst, displayPlural } from "@/lib/kind-names"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import type { ChangeOp } from "@/lib/changerequests"

/** The write verbs a policy selector matches (policy.go's policyOp*). */
export type PolicyOp = "put" | "patch" | "delete"

const POLICY_PREFIX = "substrate.reamde.dev/core/recordpatchpolicy/"

/** One "always allow" as the person means it: this agent may make these
 * writes to this kind, past the gate that held them. */
export interface AllowRule {
  /** The agent record's id, `<authority>/<package>/<name>`. */
  agent: string
  /** The full kind reference. */
  kind: string
  /** The verbs, in selector order. */
  ops: PolicyOp[]
  /** The gate policy's id. */
  gate: string
}

/** The verbs a request's op stands for at the door. */
export function opsForRequest(op: ChangeOp): PolicyOp[] {
  return op === "delete" ? ["delete"] : ["put", "patch"]
}

/** The id of the policy a stored reference names, whichever way it is
 * spelled (`{ref: <kind>/<id>}` served, the bare id authored). */
export function policyIdOf(value: unknown): string | undefined {
  const path = readReference(value)?.path.trim()
  if (!path) return undefined
  return path.startsWith(POLICY_PREFIX)
    ? path.slice(POLICY_PREFIX.length)
    : path
}

/** The rule "Always allow this" would write for a request, or undefined when
 * the request was not held by a gate or its agent or kind is unknown. */
export function allowRuleFor(opts: {
  request: SubstrateRecord
  op: ChangeOp
  agent?: string
  kind?: string
}): AllowRule | undefined {
  const gate = policyIdOf(opts.request.properties.policy)
  if (!gate || !opts.agent || !opts.kind) return undefined
  return {
    agent: opts.agent,
    kind: opts.kind,
    ops: opsForRequest(opts.op),
    gate,
  }
}

/** FNV-1a, 32 bits, as eight hex digits: a stable id part for a rule, so
 * saving the same rule twice overwrites rather than duplicates. */
function fnv(text: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16).padStart(8, "0")
}

/** One policy record to write per verb: the id and its properties. */
export function allowRuleRecords(
  rule: AllowRule
): Array<{ id: string; properties: Record<string, unknown> }> {
  const key = fnv(`${rule.agent}|${rule.kind}|${rule.gate}`)
  return rule.ops.map((op) => ({
    id: `always-allow-${key}-${op}`,
    properties: {
      selector: { kinds: [rule.kind], ops: [op], agents: [rule.agent] },
      action: "allow",
      overrides: rule.gate,
    },
  }))
}

function selectorOf(policy: SubstrateRecord): Record<string, unknown> {
  const held = policy.properties.selector
  return held && typeof held === "object" && !Array.isArray(held)
    ? (held as Record<string, unknown>)
    : {}
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string")
    : []
}

const OP_ORDER: PolicyOp[] = ["put", "patch", "delete"]

/** A standing allow the person wrote for an agent, with the policy records
 * that make it up. */
export interface StandingAllow extends AllowRule {
  records: SubstrateRecord[]
}

/** The live allows that lift a gate for one agent, one rule per kind and
 * gate, their verbs merged, in kind order. */
export function standingAllows(
  policies: SubstrateRecord[],
  agent: string
): StandingAllow[] {
  const byKey = new Map<string, StandingAllow>()
  for (const policy of policies) {
    if (policy.properties.disabled === true) continue
    if (policy.properties.action !== "allow") continue
    const gate = policyIdOf(policy.properties.overrides)
    if (!gate) continue
    const selector = selectorOf(policy)
    const agents = strings(selector.agents)
    const kinds = strings(selector.kinds)
    const ops = strings(selector.ops).filter((op): op is PolicyOp =>
      OP_ORDER.includes(op as PolicyOp)
    )
    if (agents.length !== 1 || agents[0] !== agent) continue
    if (kinds.length !== 1 || ops.length !== 1) continue
    const key = `${kinds[0]}|${gate}`
    const held = byKey.get(key) ?? {
      agent,
      kind: kinds[0],
      ops: [],
      gate,
      records: [],
    }
    if (!held.ops.includes(ops[0])) held.ops.push(ops[0])
    held.records.push(policy)
    byKey.set(key, held)
  }
  const out = [...byKey.values()]
  for (const rule of out) {
    rule.ops.sort((a, b) => OP_ORDER.indexOf(a) - OP_ORDER.indexOf(b))
  }
  return out.sort((a, b) => a.kind.localeCompare(b.kind))
}

/** Whether the standing allows already hold every verb of a rule, so
 * offering it again would write nothing new. */
export function ruleHeld(rule: AllowRule, standing: StandingAllow[]): boolean {
  return standing.some(
    (held) =>
      held.agent === rule.agent &&
      held.kind === rule.kind &&
      held.gate === rule.gate &&
      rule.ops.every((op) => held.ops.includes(op))
  )
}

/** What the verbs let the agent do to a kind, as a phrase: "add and change
 * tasks", "delete people". */
export function allowWords(kind: string, ops: PolicyOp[]): string {
  const plural = lowerFirst(displayPlural(kind))
  const verbs = allowVerbs(ops)
  return verbs ? `${verbs} ${plural}` : plural
}

/** The verbs alone, lowercase: "add and change", "delete". */
export function allowVerbs(ops: PolicyOp[]): string {
  const verbs: string[] = []
  if (ops.includes("put")) verbs.push("add")
  if (ops.includes("patch")) verbs.push("change")
  if (ops.includes("delete")) verbs.push("delete")
  const last = verbs.pop()
  if (!last) return ""
  return `${verbs.length ? `${verbs.join(", ")} and ` : ""}${last}`
}

/** The confirmation's consequence: what the agent will do from now on, and
 * where the person takes it back. */
export function allowConsequence(agentName: string, rule: AllowRule): string {
  return `${agentName} will ${allowWords(rule.kind, rule.ops)} without asking. You can take this back in the agent’s panel.`
}
