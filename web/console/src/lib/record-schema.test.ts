/** The one schema projection both editing lenses read: what a declaration says
 * a property IS (datatype, required, repeated, enum, state, reference), which
 * control it earns, what a template seeds it with, what a worked example looks
 * like, and — the point of the whole module — whether a value is one the
 * substrate will accept, decided HERE instead of by a 422. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  checkValue,
  controlFor,
  exampleFor,
  formatValue,
  parseValue,
  propSpecs,
  seedValue,
  type PropSpec,
} from "./record-schema"

/** An agent-like kind: the projection must cover it with no special-casing. */
const agentKind: KindInfo = {
  identity: "core.substrate.reamde.dev/agent",
  name: "agent",
  authority: "core.substrate.reamde.dev",
  version: 0,
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

/** Everything the datatype rules have to answer for, in one declaration. */
const wideKind: KindInfo = {
  identity: "core.substrate.reamde.dev/wide",
  name: "wide",
  authority: "core.substrate.reamde.dev",
  version: 0,
  plural: "wides",
  source: "builtin",
  definition: {
    properties: {
      apiKey: { type: "secret", description: "bearer for the endpoint" },
      baseURL: { type: "url" },
      seenAt: { type: "datetime" },
      born: { type: "date" },
      every: { type: "duration" },
      mailbox: { type: "email" },
      zone: { type: "timezone" },
      calls: { type: "int", min: 1, max: 10 },
      ratio: { type: "float" },
      price: { type: "decimal", min: 0 },
      headers: { type: "json" },
      callable: { type: "reference", kind: "any" },
      owner: { type: "reference", kind: "core.substrate.reamde.dev/actor" },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
      hidden: { type: "string", writer: "oauth" },
      code: { type: "string", pattern: "^[a-z]{3}$" },
    },
  },
}

function spec(kind: KindInfo, name: string): PropSpec {
  return propSpecs(kind).find((s) => s.name === name)!
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
    const byName = Object.fromEntries(
      propSpecs(agentKind).map((s) => [s.name, s])
    )
    expect(byName.model.values).toEqual([
      { value: "opus", label: "Opus 4" },
      { value: "sonnet", label: "Sonnet 4" },
      { value: "haiku", label: "" },
    ])
    expect(byName.model.default).toBe("opus")
    expect(byName.functions.repeated).toBe(true)
    expect(byName.enabled.required).toBe(true)
  })

  it("carries the declaration's extras: writer, range, pattern, states, to", () => {
    expect(spec(wideKind, "hidden").writer).toBe("oauth")
    expect(spec(wideKind, "calls").min).toBe(1)
    expect(spec(wideKind, "code").pattern).toBe("^[a-z]{3}$")
    expect(spec(wideKind, "status").states).toEqual([
      "proposed",
      "open",
      "done",
    ])
    expect(spec(wideKind, "owner").to).toBe("core.substrate.reamde.dev/actor")
  })

  it("labels a property from displayName, else humanizes the id", () => {
    expect(spec(wideKind, "baseURL").label).toBe("Base URL")
    expect(spec(agentKind, "maxTurns").label).toBe("Max turns")
  })
})

describe("controlFor", () => {
  it("gives every datatype the control it earns", () => {
    expect(controlFor(spec(wideKind, "apiKey"))).toBe("secret")
    expect(controlFor(spec(wideKind, "status"))).toBe("state")
    expect(controlFor(spec(wideKind, "callable"))).toBe("reference")
    expect(controlFor(spec(wideKind, "headers"))).toBe("json")
    expect(controlFor(spec(wideKind, "calls"))).toBe("number")
    // A decimal is a STRING of exact digits: a number control would round it
    // through the float64 the datatype exists to refuse.
    expect(controlFor(spec(wideKind, "price"))).toBe("text")
    expect(controlFor(spec(wideKind, "seenAt"))).toBe("datetime")
    expect(controlFor(spec(wideKind, "baseURL"))).toBe("text")
    expect(controlFor(spec(agentKind, "model"))).toBe("select")
    expect(controlFor(spec(agentKind, "functions"))).toBe("list")
    expect(controlFor(spec(agentKind, "enabled"))).toBe("bool")
    expect(controlFor(spec(agentKind, "prompt"))).toBe("prose")
  })
})

describe("seedValue", () => {
  it("prefers a declared default", () => {
    expect(seedValue(spec(agentKind, "model"))).toBe("opus")
  })
  it("seeds typed zeros / placeholders by kind", () => {
    expect(seedValue(spec(agentKind, "prompt"))).toBe("")
    expect(seedValue(spec(agentKind, "enabled"))).toBe(false)
    expect(seedValue(spec(agentKind, "functions"))).toEqual([])
    expect(seedValue(spec(wideKind, "status"))).toBe("open")
  })
})

describe("exampleFor", () => {
  it("shows a worked value per datatype, so a blank create is not a blank page", () => {
    expect(exampleFor(spec(wideKind, "seenAt"))).toBe("2026-01-31T09:00:00Z")
    expect(exampleFor(spec(wideKind, "baseURL"))).toBe("https://example.com")
    expect(exampleFor(spec(wideKind, "every"))).toBe("47m12s")
    expect(exampleFor(spec(wideKind, "price"))).toBe("19.99")
    // An enum's example is one of its own admitted values, never invented.
    expect(exampleFor(spec(agentKind, "model"))).toBe("opus")
  })
})

describe("checkValue", () => {
  it("admits what the substrate admits", () => {
    expect(
      checkValue(spec(wideKind, "seenAt"), "2026-01-31T09:00:00Z")
    ).toBeUndefined()
    expect(checkValue(spec(wideKind, "born"), "2026-01-31")).toBeUndefined()
    expect(checkValue(spec(wideKind, "every"), "47m12s")).toBeUndefined()
    // The second duration grammar: ISO 8601, minus years and months.
    expect(checkValue(spec(wideKind, "every"), "PT47M12S")).toBeUndefined()
    expect(checkValue(spec(wideKind, "every"), "P2DT3H")).toBeUndefined()
    expect(checkValue(spec(wideKind, "price"), "19.99")).toBeUndefined()
    expect(checkValue(spec(wideKind, "price"), "0.00")).toBeUndefined()
    expect(
      checkValue(spec(wideKind, "mailbox"), "a@example.com")
    ).toBeUndefined()
    expect(
      checkValue(spec(wideKind, "baseURL"), "https://example.com")
    ).toBeUndefined()
    expect(checkValue(spec(wideKind, "zone"), "Europe/London")).toBeUndefined()
    expect(checkValue(spec(wideKind, "calls"), 5)).toBeUndefined()
    expect(checkValue(spec(wideKind, "headers"), { a: 1 })).toBeUndefined()
    expect(checkValue(spec(agentKind, "functions"), ["a", "b"])).toBeUndefined()
    // A secret reads back redacted; writing the sentinel back is a no-op.
    expect(checkValue(spec(wideKind, "apiKey"), "<redacted>")).toBeUndefined()
    // Nothing is ever a problem: null is the delete marker.
    expect(checkValue(spec(wideKind, "calls"), null)).toBeUndefined()
  })

  it("names the reason a value is wrong, on the datatype's own terms", () => {
    // The issue's own example: apiKey is a string, so a bare 123 is wrong.
    expect(checkValue(spec(wideKind, "apiKey"), 123)).toMatch(/string/)
    expect(checkValue(spec(wideKind, "seenAt"), "yesterday")).toMatch(
      /timestamp/
    )
    expect(checkValue(spec(wideKind, "born"), "31/01/2026")).toMatch(
      /civil date/
    )
    expect(checkValue(spec(wideKind, "every"), "soon")).toMatch(/duration/)
    // A bare P names no duration, and years have no fixed length.
    expect(checkValue(spec(wideKind, "every"), "P")).toMatch(/duration/)
    expect(checkValue(spec(wideKind, "every"), "P1Y")).toMatch(/duration/)
    // A decimal is a string of digits: a JSON number rode float64 to get here.
    expect(checkValue(spec(wideKind, "price"), 19.99)).toMatch(/string/)
    expect(checkValue(spec(wideKind, "price"), "1.9e2")).toMatch(/decimal/)
    expect(checkValue(spec(wideKind, "price"), "-0.01")).toMatch(/>= 0/)
    expect(checkValue(spec(wideKind, "mailbox"), "nobody")).toMatch(/mailbox/)
    expect(checkValue(spec(wideKind, "baseURL"), "example.com")).toMatch(
      /absolute URL/
    )
    expect(checkValue(spec(wideKind, "zone"), "Middle/Earth")).toMatch(
      /time zone/
    )
    expect(checkValue(spec(wideKind, "calls"), "5")).toMatch(/number/)
    expect(checkValue(spec(wideKind, "calls"), 1.5)).toMatch(/integer/)
    expect(checkValue(spec(wideKind, "calls"), 99)).toMatch(/<= 10/)
    expect(checkValue(spec(wideKind, "code"), "abcd")).toMatch(/does not match/)
    expect(checkValue(spec(agentKind, "model"), "gpt")).toMatch(/opus/)
    expect(checkValue(spec(agentKind, "enabled"), 3)).toMatch(/boolean/)
    expect(checkValue(spec(wideKind, "status"), "archived")).toMatch(/proposed/)
  })

  it("checks a repeated property item by item", () => {
    expect(checkValue(spec(agentKind, "functions"), "nope")).toMatch(/list of/)
    expect(checkValue(spec(agentKind, "functions"), ["a", 2])).toMatch(/\[1\]/)
  })

  it("knows a reference is a path, and that only a pin completes a bare id", () => {
    const anyRef = spec(wideKind, "callable")
    expect(
      checkValue(anyRef, "core.substrate.reamde.dev/function/f")
    ).toBeUndefined()
    // Unpinned, a bare id names nothing: the refusal quotes it back, as the
    // engine's does.
    expect(checkValue(anyRef, "f")).toMatch(/full "<kind>\/<id>" path/)
    expect(checkValue(anyRef, "f")).toMatch(/"f"/)
    expect(checkValue(anyRef, "")).toMatch(/needs an id/)
    // The released pair is not a value any more, and says so by shape.
    expect(
      checkValue(anyRef, {
        kind: "core.substrate.reamde.dev/function",
        id: "f",
      })
    ).toMatch(/path string/)
    // A pinned kind supplies what the bare id omits, on the server and here.
    expect(checkValue(spec(wideKind, "owner"), "alice")).toBeUndefined()
    expect(
      checkValue(spec(wideKind, "owner"), "core.substrate.reamde.dev/actor/a")
    ).toBeUndefined()
  })
})

describe("parseValue / formatValue", () => {
  it("round-trips a value through its control's text", () => {
    const calls = spec(wideKind, "calls")
    expect(parseValue(calls, "7")).toEqual({ value: 7 })
    expect(formatValue(calls, 7)).toBe("7")

    const functions = spec(agentKind, "functions")
    expect(parseValue(functions, "a\nb")).toEqual({ value: ["a", "b"] })
    expect(formatValue(functions, ["a", "b"])).toBe("a\nb")

    const headers = spec(wideKind, "headers")
    expect(parseValue(headers, '{"a":1}')).toEqual({ value: { a: 1 } })
    expect(formatValue(headers, { a: 1 })).toBe('{\n  "a": 1\n}')
  })

  it("blank is not a value", () => {
    expect(parseValue(spec(wideKind, "baseURL"), "  ")).toEqual({})
    expect(parseValue(spec(agentKind, "functions"), "\n")).toEqual({})
  })

  it("hands back the datatype's complaint instead of a value", () => {
    expect(parseValue(spec(wideKind, "seenAt"), "yesterday").error).toMatch(
      /timestamp/
    )
    expect(parseValue(spec(wideKind, "headers"), "{oops").error).toMatch(/JSON/)
    expect(parseValue(spec(wideKind, "calls"), "many").error).toMatch(/number/)
  })

  it("never renders a stored secret", () => {
    expect(formatValue(spec(wideKind, "apiKey"), "<redacted>")).toBe("")
  })
})
