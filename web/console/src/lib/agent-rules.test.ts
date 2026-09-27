import { describe, expect, it } from "vitest"

import {
  allowConsequence,
  allowRuleFor,
  allowRuleRecords,
  allowWords,
  opsForRequest,
  policyIdOf,
  standingAllows,
} from "./agent-rules"
import type { SubstrateRecord } from "@/lib/api/types"

const TASK = "samples.substrate.reamde.dev/tasks/task"
const AGENT = "crew.example.com/bots/taskbot"

function policy(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/recordpatchpolicy",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("agent rules", () => {
  it("names a gated create or patch by both door verbs, a delete by one", () => {
    expect(opsForRequest("create")).toEqual(["put", "patch"])
    expect(opsForRequest("patch")).toEqual(["put", "patch"])
    expect(opsForRequest("delete")).toEqual(["delete"])
  })

  it("reads the gate's id whichever way the reference is spelled", () => {
    expect(
      policyIdOf({ ref: "substrate.reamde.dev/core/recordpatchpolicy/gate-1" })
    ).toBe("gate-1")
    expect(policyIdOf("gate-1")).toBe("gate-1")
    expect(policyIdOf(undefined)).toBeUndefined()
  })

  it("offers a rule only for a request a gate held", () => {
    const request = policy("cr-1", { policy: { ref: "gate-1" } })
    expect(
      allowRuleFor({ request, op: "patch", agent: AGENT, kind: TASK })
    ).toEqual({
      agent: AGENT,
      kind: TASK,
      ops: ["put", "patch"],
      gate: "gate-1",
    })
    expect(
      allowRuleFor({
        request: policy("cr-2", {}),
        op: "patch",
        agent: AGENT,
        kind: TASK,
      })
    ).toBeUndefined()
    expect(allowRuleFor({ request, op: "patch", kind: TASK })).toBeUndefined()
  })

  it("writes one narrow allow per verb, with a stable id", () => {
    const rule = {
      agent: AGENT,
      kind: TASK,
      ops: ["put", "patch"] as const,
      gate: "gate-1",
    }
    const records = allowRuleRecords({ ...rule, ops: [...rule.ops] })
    expect(records.map((r) => r.properties)).toEqual([
      {
        selector: { kinds: [TASK], ops: ["put"], agents: [AGENT] },
        action: "allow",
        overrides: "gate-1",
      },
      {
        selector: { kinds: [TASK], ops: ["patch"], agents: [AGENT] },
        action: "allow",
        overrides: "gate-1",
      },
    ])
    expect(records[0].id).toMatch(/^always-allow-[0-9a-f]{8}-put$/)
    expect(allowRuleRecords({ ...rule, ops: ["put"] })[0].id).toBe(
      records[0].id
    )
  })

  it("lists an agent's standing allows as one rule per kind and gate", () => {
    const put = policy("a-put", {
      action: "allow",
      overrides: { ref: "substrate.reamde.dev/core/recordpatchpolicy/gate-1" },
      selector: { kinds: [TASK], ops: ["put"], agents: [AGENT] },
    })
    const patch = policy("a-patch", {
      action: "allow",
      overrides: "gate-1",
      selector: { kinds: [TASK], ops: ["patch"], agents: [AGENT] },
    })
    const other = policy("other", {
      action: "allow",
      overrides: "gate-1",
      selector: { kinds: [TASK], ops: ["put"], agents: ["x.example.com/a/b"] },
    })
    const off = policy("off", {
      action: "allow",
      overrides: "gate-2",
      disabled: true,
      selector: { kinds: [TASK], ops: ["delete"], agents: [AGENT] },
    })
    const plain = policy("plain", {
      action: "allow",
      selector: { kinds: [TASK], ops: ["delete"], agents: [AGENT] },
    })
    const rules = standingAllows([patch, put, other, off, plain], AGENT)
    expect(rules).toHaveLength(1)
    expect(rules[0].ops).toEqual(["put", "patch"])
    expect(rules[0].records.map((r) => r.id)).toEqual(["a-patch", "a-put"])
  })

  it("says the rule in words", () => {
    expect(allowWords(TASK, ["put", "patch"])).toBe("add and change tasks")
    expect(allowWords(TASK, ["delete"])).toBe("delete tasks")
    expect(
      allowConsequence("Taskbot", {
        agent: AGENT,
        kind: TASK,
        ops: ["delete"],
        gate: "g",
      })
    ).toBe(
      "Taskbot will delete tasks without asking. You can take this back in the agent’s panel."
    )
  })
})
