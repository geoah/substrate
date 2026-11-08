import { describe, expect, it } from "vitest"

import { failingChecks } from "./checks"
import { formatDatePattern, parseDate } from "./functions"
import { childRefs } from "./tree"
import { resolve, resolveDeep, resolveText, type EvalContext } from "./values"

const model = {
  ui: { q: "dune", count: 3, email: "ada@example" },
  data: {
    books: [
      { id: "b1", properties: { title: "Dune", author: { title: "Herbert" } } },
      { id: "b2", properties: { title: "Emma", author: { ref: "x/y/z/1" } } },
    ],
  },
}

const ctx: EvalContext = { model, locale: "en-US" }

function call(name: string, args: Record<string, unknown>, at = ctx) {
  return resolve({ "@call": name, args }, at)
}

describe("dynamic values", () => {
  it("reads a literal, an absolute binding and a missing one", () => {
    expect(resolve("plain", ctx)).toBe("plain")
    expect(resolve({ "@path": "/ui/q" }, ctx)).toBe("dune")
    expect(resolve({ "@path": "/ui/nope" }, ctx)).toBeUndefined()
  })

  it("reads a relative binding in a template item's scope, and its index", () => {
    const [, second] = childRefs(
      { path: "/data/books", componentId: "row" },
      ctx
    )
    expect(second.id).toBe("row")
    expect(second.ctx.scope).toBe("/data/books/1")
    expect(resolve({ "@path": "properties/title" }, second.ctx)).toBe("Emma")
    expect(resolve({ "@path": "/ui/q" }, second.ctx)).toBe("dune")
    expect(resolve({ "@call": "@index" }, second.ctx)).toBe(1)
    expect(
      resolve({ "@call": "@index", args: { offset: 1 } }, second.ctx)
    ).toBe(2)
    // Outside a template there is no index.
    expect(resolve({ "@call": "@index" }, ctx)).toBeUndefined()
  })

  it("keys template items by their own id, and nests scopes", () => {
    const rows = childRefs({ path: "/data/books", componentId: "row" }, ctx)
    expect(rows.map((r) => r.key)).toEqual(["id:b1", "id:b2"])
    const inner = childRefs(
      { path: "properties", componentId: "field" },
      rows[0].ctx
    )
    expect(inner.map((r) => r.ctx.scope)).toEqual([
      "/data/books/0/properties/title",
      "/data/books/0/properties/author",
    ])
    expect(childRefs(["a", "b"], ctx).map((r) => r.id)).toEqual(["a", "b"])
  })

  it("answers nothing for an unknown function, never a throw", () => {
    expect(call("nope", {})).toBeUndefined()
    expect(call("formatCurrency", { value: 3, currency: "not money" })).toBe(
      undefined
    )
  })

  it("resolves an action's context deeply", () => {
    expect(
      resolveDeep(
        {
          id: { "@path": "/ui/q" },
          fixed: 1,
          list: [{ "@path": "/ui/count" }],
        },
        ctx
      )
    ).toEqual({ id: "dune", fixed: 1, list: [3] })
  })
})

describe("formatString", () => {
  const row = childRefs({ path: "/data/books", componentId: "row" }, ctx)[0].ctx

  it("interpolates absolute and relative paths", () => {
    expect(
      call(
        "formatString",
        { value: "**${properties/title}** · ${properties/author/title}" },
        row
      )
    ).toBe("**Dune** · Herbert")
    expect(call("formatString", { value: "q=${/ui/q}" })).toBe("q=dune")
  })

  it("calls functions with named args, nested expressions and literals", () => {
    expect(
      call("formatString", {
        value:
          "${/ui/count} ${pluralize(value: ${/ui/count}, one: 'book', other: 'books')}",
      })
    ).toBe("3 books")
    expect(
      call("formatString", {
        value: "${formatNumber(value: 1234.5, decimals: 1)}",
      })
    ).toBe("1,234.5")
  })

  it("keeps an escaped ${ and a missing value as nothing", () => {
    expect(call("formatString", { value: "\\${/ui/q} and ${/ui/nope}." })).toBe(
      "${/ui/q} and ."
    )
  })
})

describe("the format functions", () => {
  it("formats numbers and currency", () => {
    expect(call("formatNumber", { value: 1234567 })).toBe("1,234,567")
    expect(call("formatNumber", { value: 1234, grouping: false })).toBe("1234")
    expect(call("formatCurrency", { value: 9.5, currency: "USD" })).toBe(
      "$9.50"
    )
    expect(
      call("formatCurrency", { value: 1200, currency: "EUR", decimals: 0 })
    ).toBe("€1,200")
  })

  it("formats a date by a TR35 pattern", () => {
    expect(
      call("formatDate", { value: "2026-01-16", format: "MMM dd, yyyy" })
    ).toBe("Jan 16, 2026")
    expect(
      call("formatDate", {
        value: "2026-01-16T14:30:05",
        format: "EEEE, d MMMM yy 'at' h:mm a (HH:mm:ss)",
      })
    ).toBe("Friday, 16 January 26 at 2:30 PM (14:30:05)")
    expect(call("formatDate", { value: "not a date", format: "yyyy" })).toBe(
      undefined
    )
  })

  it("reads epoch milliseconds and a date with a zone", () => {
    const at = new Date(2026, 0, 2, 3, 4).getTime()
    expect(formatDatePattern(parseDate(at)!, "yyyy-MM-dd HH:mm")).toBe(
      "2026-01-02 03:04"
    )
    expect(parseDate("2026-01-16T14:30:00Z")?.toISOString()).toBe(
      "2026-01-16T14:30:00.000Z"
    )
  })

  it("pluralizes by the locale's category", () => {
    const p = (value: number) =>
      call("pluralize", {
        value,
        zero: "none",
        one: "one book",
        other: "books",
      })
    expect(p(0)).toBe("none")
    expect(p(1)).toBe("one book")
    expect(p(5)).toBe("books")
    expect(call("pluralize", { value: 1, other: "items" })).toBe("items")
  })

  it("joins booleans and validation results", () => {
    expect(call("and", { values: [true, { valid: true }] })).toBe(true)
    expect(call("and", { values: [true, { valid: false }] })).toBe(false)
    expect(call("or", { values: [false, true] })).toBe(true)
    expect(call("not", { value: { "@path": "/ui/missing" } })).toBe(true)
  })

  it("resolves a text prop through a call", () => {
    expect(
      resolveText(
        { "@call": "formatString", args: { value: "${/ui/count}" } },
        ctx
      )
    ).toBe("3")
  })
})

describe("checks", () => {
  it("passes or names each failing check's message", () => {
    const checks = [
      {
        condition: {
          "@call": "required",
          args: { value: { "@path": "/ui/q" } },
        },
        message: "Required.",
      },
      {
        condition: {
          "@call": "email",
          args: { value: { "@path": "/ui/email" } },
        },
        message: "Not an email.",
      },
      {
        condition: {
          "@call": "length",
          args: { value: { "@path": "/ui/q" }, max: 2 },
        },
      },
      {
        condition: {
          "@call": "numeric",
          args: { value: { "@path": "/ui/count" }, min: 1, max: 5 },
        },
        message: "Out of range.",
      },
      {
        condition: {
          "@call": "regex",
          args: { value: { "@path": "/ui/q" }, pattern: "^d" },
        },
        message: "Starts with d.",
      },
    ]
    expect(failingChecks(checks, ctx)).toEqual([
      "Not an email.",
      "This value isn’t valid.",
    ])
  })

  it("reads a validation result's own message first", () => {
    const at: EvalContext = {
      model: { v: { valid: false, message: "Expired card." } },
    }
    expect(
      failingChecks([{ condition: { "@path": "/v" }, message: "fallback" }], at)
    ).toEqual(["Expired card."])
    expect(
      failingChecks(
        [{ condition: { "@call": "required", args: { value: "" } } }],
        ctx
      )
    ).toHaveLength(1)
  })
})
