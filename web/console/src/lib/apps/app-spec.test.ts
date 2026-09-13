/** The decoder's defaults and the problems one row can answer: the size cap,
 * a reserved module key or the entry's name, an unresolved grant reference (a warning naming the
 * package, which the launcher greys on), `via` against the read kinds, the
 * SDK refusal and the `requiresAtLeast` floor. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  appSpec,
  attachesToRecord,
  missingPackages,
  viaTarget,
} from "./app-spec"
import { SDK_MAJOR, SOURCE_CAP } from "./spec"

const TASK = "ada.example.com/tasks/task"
const PROJECT = "ada.example.com/tasks/project"
const PERSON = "ada.example.com/people/person"

const kind = (
  identity: string,
  properties: Record<string, unknown>
): KindInfo => {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 3,
    source: "installed",
    description: "",
    definition: { properties },
  }
}

const kinds = [
  kind(TASK, {
    name: { type: "string" },
    project: { type: "reference", kind: "project" },
    status: { type: "state", states: ["open", "done"] },
  }),
  kind(PROJECT, { name: { type: "string" } }),
]

const kindRef = (identity: string) => ({
  ref: `substrate.reamde.dev/core/kind/${identity}`,
})

function app(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "tasks",
    kind: "substrate.reamde.dev/core/app",
    properties: {
      name: "Tasks",
      runtime: "react",
      source: "export default () => null",
      permissions: {
        reads: { kinds: [kindRef(TASK)] },
        writes: [kindRef(TASK)],
      },
      ...properties,
    },
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("appSpec", () => {
  it("fills the defaults and reads references down to identities", () => {
    const spec = appSpec(app({}), kinds)
    expect(spec.attach).toEqual(["launcher"])
    expect(spec.sdk).toBe(1)
    expect(spec.runtime).toBe("react")
    expect(spec.permissions.reads.kinds).toEqual([TASK])
    expect(spec.permissions.writes).toEqual([TASK])
    expect(spec.permissions.reads.traits).toEqual([])
    expect(spec.problems).toEqual([])
  })

  it("reads traits, functions and agents by their own prefixes", () => {
    const spec = appSpec(
      app({
        permissions: {
          reads: {
            traits: [{ ref: "substrate.reamde.dev/core/trait/temporal" }],
          },
          call: [{ ref: "substrate.reamde.dev/core/function/summarize" }],
          agents: [{ ref: "substrate.reamde.dev/core/agent/helper" }],
        },
      }),
      kinds
    )
    expect(spec.permissions.reads.traits).toEqual(["temporal"])
    expect(spec.permissions.call).toEqual(["summarize"])
    expect(spec.permissions.agents).toEqual(["helper"])
  })

  it("holds the source to the cap", () => {
    const spec = appSpec(app({ source: "x".repeat(SOURCE_CAP + 1) }), kinds)
    expect(spec.problems).toContainEqual({
      path: "source",
      message: `${SOURCE_CAP} bytes is the cap`,
      severity: "error",
    })
  })

  it("refuses a module key the import map owns and keeps the rest", () => {
    const spec = appSpec(
      app({ modules: { react: "export {}", rows: "export const n = 1" } }),
      kinds
    )
    expect(spec.modules).toEqual({ rows: "export const n = 1" })
    expect(spec.problems.map((p) => p.path)).toEqual(["modules.react"])
    expect(spec.problems[0].severity).toBe("error")
  })

  it("refuses a module spelled as the entry", () => {
    const spec = appSpec(
      app({ modules: { source: "export {}", rows: "export const n = 1" } }),
      kinds
    )
    expect(spec.modules).toEqual({ rows: "export const n = 1" })
    expect(spec.problems).toContainEqual({
      path: "modules.source",
      message: "source is the entry's name; a module has its own",
      severity: "error",
    })
  })

  it("warns on a grant reference the registry lacks, naming the package", () => {
    const spec = appSpec(
      app({
        permissions: {
          reads: { kinds: [kindRef(TASK), kindRef(PERSON)] },
        },
      }),
      kinds
    )
    expect(spec.problems).toEqual([
      {
        path: "permissions.reads.kinds[1]",
        message: "needs ada.example.com/people",
        severity: "warning",
      },
    ])
    expect(missingPackages(spec)).toEqual(["ada.example.com/people"])
  })

  it("warns when no read kind declares the via reference", () => {
    expect(appSpec(app({ via: "project" }), kinds).problems).toEqual([])
    expect(appSpec(app({ via: "owner" }), kinds).problems).toEqual([
      {
        path: "via",
        message: "no kind the app reads declares a reference called owner",
        severity: "warning",
      },
    ])
  })

  it("resolves via to the kind it points at, and attaches there alone", () => {
    const spec = appSpec(app({ via: "project", attach: ["record"] }), kinds)
    expect(viaTarget(spec, kinds)).toBe(PROJECT)
    expect(attachesToRecord(spec, kinds, PROJECT)).toBe(true)
    expect(attachesToRecord(spec, kinds, TASK)).toBe(false)
    const plain = appSpec(app({ attach: ["record"] }), kinds)
    expect(attachesToRecord(plain, kinds, TASK)).toBe(true)
    expect(attachesToRecord(plain, kinds, PROJECT)).toBe(false)
  })

  it("refuses an SDK major above the one served", () => {
    const spec = appSpec(app({ sdk: SDK_MAJOR + 1 }), kinds)
    expect(spec.problems.map((p) => p.path)).toEqual(["sdk"])
    expect(spec.problems[0].severity).toBe("error")
  })

  it("holds requiresAtLeast against the versions the repository has", () => {
    const spec = appSpec(
      app({ requiresAtLeast: { "ada.example.com/tasks": 5 } }),
      kinds,
      { "ada.example.com/tasks": 3 }
    )
    expect(spec.problems.map((p) => p.path)).toEqual([
      "requiresAtLeast.ada.example.com/tasks",
    ])
    expect(
      appSpec(app({ requiresAtLeast: { "ada.example.com/tasks": 5 } }), kinds)
        .problems
    ).toEqual([])
  })

  it("keeps inputs and bindings, and warns on a grant left out", () => {
    const spec = appSpec(
      app({
        permissions: undefined,
        inputs: { me: { kind: kindRef(PERSON), description: "whose" } },
        bindings: { me: { ref: `${PERSON}/ada` } },
      }),
      kinds
    )
    expect(spec.inputs).toEqual({ me: { kind: PERSON, description: "whose" } })
    expect(spec.bindings).toEqual({ me: `${PERSON}/ada` })
    expect(spec.problems.map((p) => p.path)).toEqual(["permissions"])
  })
})
