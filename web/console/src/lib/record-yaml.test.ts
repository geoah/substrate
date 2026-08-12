import { parse } from "yaml"
import { describe, expect, it } from "vitest"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  applyManifestYAML,
  parseApplyDoc,
  propSpecs,
  seedValue,
  templateYAML,
  toPutInput,
  validateApplyDoc,
} from "./record-yaml"

/** An agent-like kind: the schema-driven template must cover it with no
 * kind-special-casing (it is a brand-new core kind). */
const agentKind: KindInfo = {
  identity: "core.substrate.reamde.dev/agent",
  name: "agent",
  authority: "core.substrate.reamde.dev",
  version: "",
  plural: "agents",
  source: "builtin",
  definition: {
    properties: {
      prompt: { type: "text", required: true, description: "the system prompt" },
      model: {
        type: "string",
        default: "opus",
        values: [
          { value: "opus", label: "Opus 4" },
          { value: "sonnet", label: "Sonnet 4" },
          { value: "haiku", label: "" },
        ],
      },
      functions: { type: "string", repeated: true },
      subagents: { type: "string", repeated: true },
      maxTurns: { type: "int", default: 8 },
      enabled: { type: "bool", required: true },
    },
  },
}

describe("propSpecs", () => {
  it("orders required first, then alphabetical within each band", () => {
    const names = propSpecs(agentKind).map((s) => s.name)
    expect(names).toEqual([
      "enabled",
      "prompt",
      "functions",
      "maxTurns",
      "model",
      "subagents",
    ])
  })

  it("carries enum values, defaults and repeated off the raw manifest", () => {
    const byName = Object.fromEntries(propSpecs(agentKind).map((s) => [s.name, s]))
    expect(byName.model.values).toEqual([
      { value: "opus", label: "Opus 4" },
      { value: "sonnet", label: "Sonnet 4" },
      { value: "haiku", label: "" },
    ])
    expect(byName.model.default).toBe("opus")
    expect(byName.functions.repeated).toBe(true)
    expect(byName.enabled.required).toBe(true)
  })
})

describe("seedValue", () => {
  it("prefers a declared default", () => {
    const model = propSpecs(agentKind).find((s) => s.name === "model")!
    expect(seedValue(model)).toBe("opus")
  })
  it("seeds typed zeros / placeholders by kind", () => {
    const by = Object.fromEntries(propSpecs(agentKind).map((s) => [s.name, s]))
    expect(seedValue(by.prompt)).toBe("") // text
    expect(seedValue(by.enabled)).toBe(false) // bool
    expect(seedValue(by.functions)).toEqual([]) // repeated
  })
})

describe("templateYAML", () => {
  const yaml = templateYAML(agentKind)

  it("builds the full apply-able envelope fixed to the kind reference", () => {
    const doc = parse(yaml)
    expect(doc.kind).toBe("core.substrate.reamde.dev/agent")
    expect(doc.metadata).toHaveProperty("id")
    expect(doc.data.properties).toMatchObject({
      prompt: "",
      enabled: false,
      model: "opus",
      maxTurns: 8,
      functions: [],
    })
  })

  it("comments required vs optional and lists enum values (value + authored label)", () => {
    expect(yaml).toContain("# required, text")
    expect(yaml).toContain(
      "# optional, enum: opus (Opus 4) | sonnet (Sonnet 4) | haiku"
    )
    expect(yaml).toContain("# optional, string[]")
    expect(yaml).toMatch(/id: ""\s*# optional/)
  })

  it("keeps an empty seed list inline with its comment", () => {
    expect(yaml).toMatch(/functions: \[\]\s*# optional, string\[\]/)
  })

  it("round-trips: its own template validates clean but for required blanks", () => {
    const problems = validateApplyDoc(yaml, agentKind)
    const errorPaths = problems
      .filter((p) => p.severity === "error")
      .map((p) => p.path)
    expect(errorPaths).toEqual(["prompt"]) // enabled=false is a valid value
  })
})

describe("validateApplyDoc", () => {
  it("reports a YAML syntax error with its line", () => {
    const bad = "kind: core.substrate.reamde.dev/agent\ndata:\n  properties:\n    a: [1, 2"
    const problems = validateApplyDoc(bad, agentKind)
    expect(problems).toHaveLength(1)
    expect(problems[0].severity).toBe("error")
    expect(problems[0].line).toBeTypeOf("number")
  })

  it("flags missing required properties", () => {
    const yaml = "data:\n  properties:\n    model: opus\n"
    const errs = validateApplyDoc(yaml, agentKind).filter(
      (p) => p.severity === "error"
    )
    expect(errs.map((p) => p.path).sort()).toEqual(["enabled", "prompt"])
  })

  it("flags an inadmissible enum value", () => {
    const yaml =
      "data:\n  properties:\n    prompt: hi\n    enabled: true\n    model: gpt\n"
    const problems = validateApplyDoc(yaml, agentKind)
    const model = problems.find((p) => p.path === "model")
    expect(model?.severity).toBe("error")
    expect(model?.message).toContain("opus")
  })

  it("admits a valid enum value", () => {
    const yaml =
      "data:\n  properties:\n    prompt: hi\n    enabled: true\n    model: sonnet\n"
    const errs = validateApplyDoc(yaml, agentKind).filter(
      (p) => p.severity === "error"
    )
    expect(errs).toHaveLength(0)
  })

  it("flags obvious kind mismatches (number where bool is declared, scalar where list is)", () => {
    const yaml =
      "data:\n  properties:\n    prompt: hi\n    enabled: 3\n    functions: nope\n"
    const problems = validateApplyDoc(yaml, agentKind)
    expect(problems.find((p) => p.path === "enabled")?.severity).toBe("error")
    expect(problems.find((p) => p.path === "functions")?.severity).toBe("error")
  })

  it("warns (does not bar) on an undeclared property", () => {
    const yaml =
      "data:\n  properties:\n    prompt: hi\n    enabled: true\n    bogus: 1\n"
    const problems = validateApplyDoc(yaml, agentKind)
    const bogus = problems.find((p) => p.path === "bogus")
    expect(bogus?.severity).toBe("warning")
    expect(problems.filter((p) => p.severity === "error")).toHaveLength(0)
  })

  it("rejects a non-mapping document", () => {
    const problems = validateApplyDoc("- a\n- b\n", agentKind)
    expect(problems[0].severity).toBe("error")
  })
})

describe("applyManifestYAML (edit seed)", () => {
  const record: SubstrateRecord = {
    id: "contactssync.google",
    kind: "core.substrate.reamde.dev/agent",
    properties: { prompt: "sync my contacts", enabled: true, model: "opus" },
    labels: { tier: "core" },
    version: 4,
    createdAt: "2026-08-05T16:26:27.161544Z",
    updatedAt: "2026-08-06T10:00:00.000000Z",
    edges: {
      uses: [{ id: "gcal.fn", kind: "core.substrate.reamde.dev/function", title: "gcal" }],
    },
    propertyMeta: {
      prompt: { manager: "owner", tier: "owner", updatedAt: "x" },
    },
  }

  it("emits the apply envelope WITHOUT the server-owned status block", () => {
    const yaml = applyManifestYAML(record)
    const doc = parse(yaml)
    expect(doc.kind).toBe("core.substrate.reamde.dev/agent")
    expect(doc.metadata.id).toBe("contactssync.google")
    expect(doc.data.properties.prompt).toBe("sync my contacts")
    // Labels ride in metadata now (the v1 envelope).
    expect(doc.metadata.labels).toMatchObject({ tier: "core" })
    expect(doc.data.edges[0]).toMatchObject({
      rel: "uses",
      to: { kind: "core.substrate.reamde.dev/function", id: "gcal.fn" },
    })
    // status / propertyMeta / version never seed the editor.
    expect(doc).not.toHaveProperty("status")
    expect(yaml).not.toContain("propertyMeta")
    expect(yaml).not.toContain("version")
  })

  it("its own seed validates clean against the schema", () => {
    const errs = validateApplyDoc(applyManifestYAML(record), agentKind).filter(
      (p) => p.severity === "error"
    )
    expect(errs).toHaveLength(0)
  })
})

describe("toPutInput", () => {
  it("extracts id, properties, labels and edges from a parsed doc", () => {
    const yaml = applyManifestYAML({
      id: "e1",
      kind: "core.substrate.reamde.dev/agent",
      properties: { prompt: "hi", enabled: true },
      labels: { a: "b" },
      version: 1,
      createdAt: "x",
      updatedAt: "x",
      edges: {
        uses: [{ id: "t1", kind: "core.substrate.reamde.dev/function" }],
      },
    })
    const parsed = parseApplyDoc(yaml)
    const input = toPutInput(parsed.value!)
    expect(input.id).toBe("e1")
    expect(input.properties).toMatchObject({ prompt: "hi", enabled: true })
    expect(input.labels).toMatchObject({ a: "b" })
    expect(input.edges).toEqual([
      { rel: "uses", to: { id: "t1", kind: "core.substrate.reamde.dev/function" } },
    ])
  })

  it("omits a blank metadata.id so the substrate mints one on create", () => {
    const input = toPutInput({ metadata: { id: "" }, data: { properties: { a: 1 } } })
    expect(input.id).toBeUndefined()
    expect(input.properties).toEqual({ a: 1 })
  })
})
