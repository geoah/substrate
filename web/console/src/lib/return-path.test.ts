// @vitest-environment jsdom
import { describe, expect, it } from "vitest"

import { returnAfterExpiry, safeReturnPath } from "./return-path"

describe("safeReturnPath", () => {
  it.each([
    "/",
    "/authorize/abc",
    "/authorize/abc?x=1",
    "/authorize/AbC123-_x",
    "/data/ada.example.com/tasks/task",
    "/search?q=2024",
    "/data/ada.example.com/tasks/task/a:b@c",
    "/history#top",
  ])("keeps the local path %s", (path) => {
    expect(safeReturnPath(path)).toBe(path)
  })

  it("answers the resolved path, not the input", () => {
    expect(safeReturnPath("/data/../authorize/abc")).toBe("/authorize/abc")
    expect(safeReturnPath("/authorize/./abc?x=1#y")).toBe(
      "/authorize/abc?x=1#y"
    )
  })

  it.each([
    ["a protocol-relative URL", "//evil.example"],
    ["three slashes", "///evil.example"],
    ["a backslash after the slash", "/\\evil.example/x"],
    ["a backslash later on", "/a\\b"],
    ["an encoded backslash", "/%5Cevil.example"],
    ["an encoded backslash, lower case", "/%5cevil.example"],
    ["an absolute URL", "https://evil.example"],
    ["an absolute URL with a path", "https://evil.example/authorize/x"],
    ["a javascript URL", "javascript:alert(1)"],
    ["a relative path", "authorize/x"],
    ["an empty string", ""],
    ["a tab that folds into //", "/\t/evil.example"],
    ["a newline", "/authorize/x\nSet-Cookie: a=b"],
    ["a space", "/authorize/a b"],
    ["a no-break space", "/authorize/a b"],
    ["a NUL", "/authorize/\u0000"],
    ["a DEL", "/authorize/\u007f"],
    ["dot segments that fold into //", "/.//evil.example"],
  ])("refuses %s", (_name, value) => {
    expect(safeReturnPath(value)).toBeUndefined()
  })

  it.each([undefined, null, 42, { to: "/" }, ["/"]])(
    "refuses the non-string %j",
    (value) => {
      expect(safeReturnPath(value)).toBeUndefined()
    }
  )
})

describe("returnAfterExpiry", () => {
  it("returns to an approval page", () => {
    expect(returnAfterExpiry("/authorize/AbC123")).toBe("/authorize/AbC123")
  })

  it("lands everywhere else on Home", () => {
    expect(returnAfterExpiry("/data/ada.example.com")).toBeUndefined()
    expect(returnAfterExpiry("/authorize")).toBeUndefined()
    expect(returnAfterExpiry("/")).toBeUndefined()
  })
})
