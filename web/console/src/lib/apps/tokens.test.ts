/** The two tokens and the escape: `$input.<name>[.<property>]` against the
 * app's inputs, `$record[.<property>]` against the clicked row, `$$` as the
 * literal dollar, and a token that cannot resolve as a Problem, never a
 * throw. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ViewContext } from "./spec"
import {
  parseToken,
  resolveToken,
  substituteFilter,
  substituteSet,
  tokenInputs,
  usesRecord,
} from "./tokens"

const account: SubstrateRecord = {
  id: "ghmain",
  kind: "providers.example.com/github/account",
  properties: {
    login: "ada",
    owner: { ref: "providers.example.com/github/user/ada" },
    members: [
      { ref: "providers.example.com/github/user/ada" },
      { ref: "providers.example.com/github/user/bob" },
    ],
  },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
}

const row: SubstrateRecord = {
  id: "t1",
  kind: "ada.example.com/tasks/task",
  properties: { name: "Call", priority: "high" },
  labels: {},
  version: 3,
  createdAt: "",
  updatedAt: "",
}

const task: KindInfo = {
  identity: "ada.example.com/tasks/task",
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      project: { type: "reference", kind: "project" },
      estimate: { type: "int" },
      note: { type: "string" },
    },
  },
}

const ctx = (inputs: ViewContext["inputs"]): ViewContext => ({
  inputs,
  mode: "page",
})

describe("parseToken", () => {
  it("reads the two scopes and one hop", () => {
    expect(parseToken("$input.me")).toEqual({ scope: "input", input: "me" })
    expect(parseToken("$input.me.login")).toEqual({
      scope: "input",
      input: "me",
      property: "login",
    })
    expect(parseToken("$record")).toEqual({ scope: "record" })
    expect(parseToken("$record.priority")).toEqual({
      scope: "record",
      property: "priority",
    })
  })
  it("refuses a second hop and a non-token", () => {
    expect(parseToken("$input.me.a.b")).toBeUndefined()
    expect(parseToken("$record.a.b")).toBeUndefined()
    expect(parseToken("plain")).toBeUndefined()
    expect(parseToken("$$input.me")).toBeUndefined()
  })
})

describe("resolveToken", () => {
  it("resolves an input to its path and a hop to its property", () => {
    const c = ctx({ me: account })
    expect(resolveToken("$input.me", c)).toEqual({
      value: "providers.example.com/github/account/ghmain",
    })
    expect(resolveToken("$input.me.login", c)).toEqual({ value: "ada" })
  })
  it("yields a reference as its path, since a filter compares one by path", () => {
    const c = ctx({ me: account })
    expect(resolveToken("$input.me.owner", c)).toEqual({
      value: "providers.example.com/github/user/ada",
    })
    expect(resolveToken("$input.me.members", c)).toEqual({
      value: [
        "providers.example.com/github/user/ada",
        "providers.example.com/github/user/bob",
      ],
    })
  })
  it("names the problem instead of throwing", () => {
    expect(resolveToken("$input.me", ctx({})).problem).toMatch(/no app/)
    expect(resolveToken("$input.me", ctx({ me: undefined })).problem).toBe(
      "`me` has no record yet"
    )
    expect(resolveToken("$input.me.email", ctx({ me: account })).problem).toBe(
      "`me` has no `email` yet (the account has not synced)"
    )
    expect(
      resolveToken(
        "$input.me.email",
        ctx({ me: account }),
        undefined,
        "this screen cannot filter on it"
      ).problem
    ).toBe(
      "`me` has no `email` yet, so this screen cannot filter on it (the account has not synced)"
    )
    expect(resolveToken("$record", ctx({})).problem).toMatch(/needs a row/)
  })
  it("resolves the row where there is one", () => {
    expect(resolveToken("$record", ctx({}), row)).toEqual({
      value: "ada.example.com/tasks/task/t1",
    })
    expect(resolveToken("$record.priority", ctx({}), row)).toEqual({
      value: "high",
    })
  })
})

describe("substituteFilter", () => {
  it("replaces tokens in every operator and unwraps the escape", () => {
    const { filter, problems } = substituteFilter(
      {
        properties: {
          account: { eq: "$input.me" },
          author: { in: ["$input.me.login", "bob"] },
          label: { prefix: "$$input" },
        },
      },
      ctx({ me: account })
    )
    expect(problems).toEqual([])
    expect(filter.properties?.account.eq).toBe(
      "providers.example.com/github/account/ghmain"
    )
    expect(filter.properties?.author.in).toEqual(["ada", "bob"])
    expect(filter.properties?.label.prefix).toBe("$input")
  })
  it("reports a $record inside a filter as a problem", () => {
    const { problems } = substituteFilter(
      { properties: { owner: { eq: "$record" } } },
      ctx({})
    )
    expect(problems).toHaveLength(1)
    expect(problems[0].path).toBe("filter.properties.owner.eq")
  })
  it("says what the screen cannot do without the value", () => {
    const { problems } = substituteFilter(
      { properties: { author: { eq: "$input.me.email" } } },
      ctx({ me: account })
    )
    expect(problems[0].message).toBe(
      "`me` has no `email` yet, so this screen cannot filter on it (the account has not synced)"
    )
  })
})

describe("substituteSet", () => {
  it("wraps a reference in {ref} and coerces by datatype", () => {
    const { properties, problems } = substituteSet(
      { project: "$record", estimate: "3", note: "$$literal" },
      ctx({}),
      task,
      "set",
      row
    )
    expect(problems).toEqual([])
    expect(properties).toEqual({
      project: { ref: "ada.example.com/tasks/task/t1" },
      estimate: 3,
      note: "$literal",
    })
  })
  it("skips the key whose token cannot resolve and says why", () => {
    const { properties, problems } = substituteSet(
      { project: "$input.me", note: "x" },
      ctx({}),
      task
    )
    expect(properties).toEqual({ note: "x" })
    expect(problems[0].path).toBe("set.project")
  })
})

describe("tokenInputs and usesRecord", () => {
  it("lists the inputs a view names, ignoring $record", () => {
    const names = tokenInputs({
      filter: { properties: { a: { eq: "$input.me" }, b: { eq: "$record" } } },
      actions: [
        {
          name: "x",
          label: "X",
          verb: "patch",
          placement: "row",
          prompt: [],
          set: { c: "$input.other.login" },
          confirm: false,
        },
      ],
    } as never)
    expect(names.sort()).toEqual(["me", "other"])
    expect(usesRecord({ c: "$record.priority" })).toBe(true)
    expect(usesRecord({ c: "$$record" })).toBe(false)
  })
})
