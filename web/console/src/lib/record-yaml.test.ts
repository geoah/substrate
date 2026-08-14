import { parse } from "yaml"
import { describe, expect, it } from "vitest"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  applyManifestYAML,
  deleteIn,
  formatYAML,
  parseApplyDoc,
  propertiesOf,
  setIn,
  templateYAML,
  toPutInput,
  validateApplyDoc,
} from "./record-yaml"

/** An agent-like kind: the schema-driven template must cover it with no
 * kind-special-casing. Published by somebody else's authority on purpose, so
 * these cases exercise the ORDINARY rules; the nine declaration kinds carry an
 * id rule of their own, which has its own block below. */
const agentKind: KindInfo = {
  identity: "crew.test.dev/agent",
  name: "agent",
  authority: "crew.test.dev",
  version: "",
  plural: "agents",
  source: "builtin",
  definition: {
    properties: {
      prompt: {
        type: "text",
        required: true,
        description: "the system prompt",
      },
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

describe("templateYAML", () => {
  const yaml = templateYAML(agentKind)

  it("builds the full apply-able envelope fixed to the kind reference", () => {
    const doc = parse(yaml)
    expect(doc.kind).toBe("crew.test.dev/agent")
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

  it("asks for a REQUIRED field inside an object, naming the deep path", () => {
    const kindKind: KindInfo = {
      identity: "core.substrate.reamde.dev/kind",
      name: "kind",
      authority: "core.substrate.reamde.dev",
      version: "",
      plural: "kinds",
      source: "builtin",
      definition: {
        properties: {
          authority: { type: "string", required: true },
          names: {
            type: "object",
            fields: {
              singular: { type: "string", required: true },
              plural: { type: "string", required: true },
            },
          },
        },
      },
    }
    const doc = `kind: core.substrate.reamde.dev/kind
metadata:
  id: tasks.example.com/task
data:
  properties:
    authority: tasks.example.com
    names:
      singular: task
`
    const messages = validateApplyDoc(doc, kindKind)
      .filter((p) => p.severity === "error")
      .map((p) => p.message)
    // `{}` used to sail through: only the fields a value CARRIES were checked.
    expect(messages).toContain("`names.plural` is required.")
    expect(
      validateApplyDoc(
        doc.replace("singular: task", "singular: task\n      plural: tasks"),
        kindKind
      ).filter((p) => p.severity === "error")
    ).toEqual([])
  })

  it("never seeds a MANAGED property: the engine stamps it, and refuses a disagreement", () => {
    const stamped = templateYAML({
      ...agentKind,
      definition: {
        properties: {
          version: { type: "string", managed: true },
          model: { type: "string" },
        },
      },
    })
    const doc = parse(stamped)
    expect(doc.data.properties).not.toHaveProperty("version")
    expect(doc.data.properties).toHaveProperty("model")
  })
})

describe("validateApplyDoc", () => {
  it("reports a YAML syntax error with its line", () => {
    const bad = "kind: crew.test.dev/agent\ndata:\n  properties:\n    a: [1, 2"
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

/** A DECLARATION is addressed by the identity it declares. There is no id to
 * mint, so a create without one is refused by the substrate, and said here
 * while the field is still on the screen. */
describe("validateApplyDoc: the declaration id", () => {
  const declaration: KindInfo = {
    identity: "core.substrate.reamde.dev/agent",
    name: "agent",
    authority: "core.substrate.reamde.dev",
    version: "",
    plural: "agents",
    source: "builtin",
    definition: { properties: { model: { type: "string" } } },
  }
  const withId = `kind: core.substrate.reamde.dev/agent
metadata:
  id: crew.test.dev/scout
data:
  properties:
    model: opus
`
  const blank = withId.replace("id: crew.test.dev/scout", 'id: ""')

  function errors(text: string, ctx = {}) {
    return validateApplyDoc(text, declaration, ctx)
      .filter((p) => p.severity === "error")
      .map((p) => p.path)
  }

  it("bars a create that leaves it blank", () => {
    expect(errors(blank)).toEqual(["metadata.id"])
    expect(errors(withId)).toEqual([])
  })

  it("says nothing on an EDIT: the id is the route's, not the document's", () => {
    const record: SubstrateRecord = {
      id: "crew.test.dev/scout",
      kind: declaration.identity,
      properties: {},
      labels: {},
      version: 1,
      createdAt: "x",
      updatedAt: "x",
    }
    expect(errors(blank, { record })).toEqual([])
  })

  it("leaves an ORDINARY kind's create alone: the substrate mints that one", () => {
    const ordinary = { ...declaration, identity: "crew.test.dev/agent" }
    expect(
      validateApplyDoc(
        blank.replace(
          "kind: core.substrate.reamde.dev/agent",
          "kind: crew.test.dev/agent"
        ),
        ordinary
      ).filter((p) => p.severity === "error")
    ).toEqual([])
  })
})

describe("applyManifestYAML (edit seed)", () => {
  const record: SubstrateRecord = {
    id: "contactssync.google",
    kind: "crew.test.dev/agent",
    properties: { prompt: "sync my contacts", enabled: true, model: "opus" },
    labels: { tier: "core" },
    version: 4,
    createdAt: "2026-08-05T16:26:27.161544Z",
    updatedAt: "2026-08-06T10:00:00.000000Z",
    edges: {
      uses: [
        {
          id: "gcal.fn",
          kind: "core.substrate.reamde.dev/function",
          title: "gcal",
        },
      ],
    },
    propertyMeta: {
      prompt: { manager: "owner", tier: "owner", updatedAt: "x" },
    },
  }

  it("emits the apply envelope WITHOUT the server-owned status block", () => {
    const yaml = applyManifestYAML(record)
    const doc = parse(yaml)
    expect(doc.kind).toBe("crew.test.dev/agent")
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
      kind: "crew.test.dev/agent",
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
      {
        rel: "uses",
        to: { id: "t1", kind: "core.substrate.reamde.dev/function" },
      },
    ])
  })

  it("omits a blank metadata.id so the substrate mints one on create", () => {
    const input = toPutInput({
      metadata: { id: "" },
      data: { properties: { a: 1 } },
    })
    expect(input.id).toBeUndefined()
    expect(input.properties).toEqual({ a: 1 })
  })
})

/** The kind whose datatypes the editor has to be honest about, and the state
 * machine a put may not move. */
const taskKind: KindInfo = {
  identity: "tasks.substrate.reamde.dev/task",
  name: "task",
  authority: "tasks.substrate.reamde.dev",
  version: "",
  plural: "tasks",
  source: "installed",
  definition: {
    properties: {
      title: { type: "string", required: true },
      dueAt: { type: "datetime" },
      endpoint: { type: "url" },
      apiKey: { type: "secret" },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
    },
    edges: {
      assignee: { to: "people.substrate.reamde.dev/person" },
    },
  },
}

const openTask: SubstrateRecord = {
  id: "t1",
  kind: "tasks.substrate.reamde.dev/task",
  properties: { title: "write it", status: "open" },
  labels: {},
  version: 2,
  createdAt: "x",
  updatedAt: "x",
}

describe("validateApplyDoc: the datatypes and the write's own rules", () => {
  it("names a value its datatype refuses, on the line it sits on", () => {
    const yaml =
      "data:\n  properties:\n    title: hi\n    dueAt: yesterday\n    endpoint: example.com\n"
    const problems = validateApplyDoc(yaml, taskKind)
    const due = problems.find((p) => p.path === "dueAt")
    expect(due?.severity).toBe("error")
    expect(due?.message).toMatch(/timestamp/)
    expect(due?.line).toBe(4)
    expect(problems.find((p) => p.path === "endpoint")?.message).toMatch(
      /absolute URL/
    )
  })

  it("passes the redaction sentinel a read handed back", () => {
    const yaml = "data:\n  properties:\n    title: hi\n    apiKey: <redacted>\n"
    expect(
      validateApplyDoc(yaml, taskKind).filter((p) => p.severity === "error")
    ).toHaveLength(0)
  })

  it("refuses a kind that is not this collection's", () => {
    const yaml =
      "kind: crew.test.dev/agent\ndata:\n  properties:\n    title: hi\n"
    const problem = validateApplyDoc(yaml, taskKind).find(
      (p) => p.path === "kind"
    )
    expect(problem?.severity).toBe("error")
    expect(problem?.line).toBe(1)
  })

  it("bars a state MOVE on an edit, because a put may not move one", () => {
    const yaml = "data:\n  properties:\n    title: hi\n    status: done\n"
    const moved = validateApplyDoc(yaml, taskKind, { record: openTask }).find(
      (p) => p.path === "status"
    )
    expect(moved?.severity).toBe("error")
    expect(moved?.message).toMatch(/transition/)
    // The same document on a CREATE is fine: a record may be born in any state.
    expect(
      validateApplyDoc(yaml, taskKind).filter((p) => p.severity === "error")
    ).toHaveLength(0)
    // And leaving the state where it stands is fine on an edit too.
    const held = "data:\n  properties:\n    title: hi\n    status: open\n"
    expect(
      validateApplyDoc(held, taskKind, { record: openTask }).filter(
        (p) => p.severity === "error"
      )
    ).toHaveLength(0)
  })

  it("warns that metadata.id does not rename the record being edited", () => {
    const yaml = "metadata:\n  id: t2\ndata:\n  properties:\n    title: hi\n"
    const problem = validateApplyDoc(yaml, taskKind, { record: openTask }).find(
      (p) => p.path === "metadata.id"
    )
    expect(problem?.severity).toBe("warning")
    expect(problem?.message).toContain("t1")
  })

  it("checks the edge list's shape and warns on an undeclared rel", () => {
    const yaml =
      "data:\n  properties:\n    title: hi\n  edges:\n    - rel: assignee\n      to: {id: p1}\n    - rel: bogus\n      to: {id: p2}\n    - rel: assignee\n      to: {}\n"
    const problems = validateApplyDoc(yaml, taskKind)
    expect(problems.find((p) => p.path === "edges[1]")?.severity).toBe(
      "warning"
    )
    expect(problems.find((p) => p.path === "edges[2]")?.severity).toBe("error")
  })
})

describe("the document as both lenses' truth", () => {
  const template = templateYAML(taskKind)

  it("setIn changes one key and leaves every other line as authored", () => {
    const next = setIn(template, ["data", "properties", "title"], "write it")
    expect(propertiesOf(next)?.title).toBe("write it")
    // The template's annotation on that very line survives being filled in.
    expect(next).toMatch(/title: write it\s*# required, string/)
    // Every other line is untouched.
    const before = template.split("\n").filter((l) => !l.includes("title:"))
    const after = next.split("\n").filter((l) => !l.includes("title:"))
    expect(after).toEqual(before)
  })

  it("setIn writes a value that needs quoting without breaking the document", () => {
    const next = setIn(template, ["data", "properties", "title"], "a: b #c")
    expect(propertiesOf(next)?.title).toBe("a: b #c")
  })

  it("deleteIn removes a key, and propertiesOf reads what is left", () => {
    const next = deleteIn(template, ["data", "properties", "dueAt"])
    expect(propertiesOf(next)).not.toHaveProperty("dueAt")
    expect(propertiesOf(next)).toHaveProperty("title")
  })

  it("leaves a document it cannot parse exactly as it is", () => {
    const broken = "data:\n  properties:\n    a: [1, 2"
    expect(setIn(broken, ["data", "properties", "a"], 1)).toBe(broken)
    expect(propertiesOf(broken)).toBeUndefined()
    expect(formatYAML(broken).error).toBeTruthy()
    expect(formatYAML(broken).text).toBe(broken)
  })

  it("formats a hand-mangled document back into shape, comments intact", () => {
    const mangled =
      "kind: tasks.substrate.reamde.dev/task\ndata:\n      properties:\n            title: hi # mine\n"
    const { text, error } = formatYAML(mangled)
    expect(error).toBeUndefined()
    expect(text).toContain("    title: hi # mine")
  })
})

describe("toPutInput: blank template lines", () => {
  it("leaves out the blanks a create never filled in", () => {
    const parsed = parseApplyDoc(templateYAML(taskKind))
    const props = toPutInput(parsed.value!, taskKind).properties ?? {}
    // `url`, `secret` and `datetime` have no empty form the substrate accepts,
    // so an untouched line is not written at all.
    expect(props).not.toHaveProperty("dueAt")
    expect(props).not.toHaveProperty("endpoint")
    expect(props).not.toHaveProperty("apiKey")
    // A plain string's empty IS a value somebody could mean.
    expect(props).toHaveProperty("title", "")
  })
})
