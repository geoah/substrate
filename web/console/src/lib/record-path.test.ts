/** The console's half of `internal/vocabulary`'s record-path grammar. The cases
 * are the Go suite's (`TestSplitRecordPath`), deliberately: the two splits are
 * a mirror, and a case that passes there and fails here is the drift this file
 * exists to catch. */

import { describe, expect, it } from "vitest"

import { coerceReferencePath, recordPath, splitRecordPath } from "./record-path"

describe("splitRecordPath", () => {
  it("splits a qualified kind's path on the kind grammar, not a registry", () => {
    expect(
      splitRecordPath("core.substrate.reamde.dev/llmprovider/claude")
    ).toEqual({ kind: "core.substrate.reamde.dev/llmprovider", id: "claude" })
  })

  it("refuses a dotless first segment: every kind carries an authority", () => {
    expect(splitRecordPath("task/abc123")).toBeUndefined()
  })

  it("keeps the slashes inside an id: a declaration id IS a kind reference", () => {
    expect(
      splitRecordPath(
        "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task"
      )
    ).toEqual({
      kind: "core.substrate.reamde.dev/kind",
      id: "tasks.substrate.reamde.dev/task",
    })
    expect(splitRecordPath("core.substrate.reamde.dev/note/a/b/c")).toEqual({
      kind: "core.substrate.reamde.dev/note",
      id: "a/b/c",
    })
  })

  it("refuses everything that is not a path, so a pin can complete it", () => {
    // The authored short form: a bare id, and the bare id of a DECLARATION,
    // which looks like a path but has nothing left after its kind.
    expect(splitRecordPath("claude")).toBeUndefined()
    expect(splitRecordPath("tasks.substrate.reamde.dev/task")).toBeUndefined()
    expect(splitRecordPath("")).toBeUndefined()
    expect(splitRecordPath("/claude")).toBeUndefined()
    expect(splitRecordPath("task/")).toBeUndefined()
    expect(splitRecordPath("core.substrate.reamde.dev/")).toBeUndefined()
  })

  it("round-trips through recordPath", () => {
    for (const path of [
      "core.substrate.reamde.dev/llmprovider/claude",
      "core.substrate.reamde.dev/note/a/b/c",
      "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task",
    ]) {
      const parts = splitRecordPath(path)
      expect(parts && recordPath(parts.kind, parts.id)).toBe(path)
    }
  })
})

/** The console's half of the write path's own coercion
 * (`engine.coerceReferencePath`). The cases are the ones the hardening added:
 * a value that reads two ways is refused naming BOTH, an empty-segment shape
 * names no record, and a slash-bearing value that does not parse as a path is
 * still the ordinary short form, which is what keeps an identity spellable. */
describe("coerceReferencePath", () => {
  const FUNCTION = "core.substrate.reamde.dev/function"
  const KIND = "core.substrate.reamde.dev/kind"

  it("leaves a full path under its own pin alone", () => {
    expect(
      coerceReferencePath(
        FUNCTION,
        `${FUNCTION}/core.substrate.reamde.dev/graphql`
      )
    ).toEqual({ value: `${FUNCTION}/core.substrate.reamde.dev/graphql` })
  })

  it("completes a bare id from the pin", () => {
    expect(
      coerceReferencePath("core.substrate.reamde.dev/llmprovider", "claude")
    ).toEqual({
      value: "core.substrate.reamde.dev/llmprovider/claude",
    })
  })

  it("keeps a slash-bearing SHORT FORM, because it parses as no path", () => {
    // `core.substrate.reamde.dev/graphql` has a dotted first segment and
    // nothing left after its kind, so it cannot be read as a path: it is the
    // identity, and the pin completes it. This is what a tool entry writes.
    expect(
      coerceReferencePath(FUNCTION, "core.substrate.reamde.dev/graphql")
    ).toEqual({ value: `${FUNCTION}/core.substrate.reamde.dev/graphql` })
    expect(coerceReferencePath(KIND, "web.example.com/page")).toEqual({
      value: `${KIND}/web.example.com/page`,
    })
  })

  it("REFUSES a value that reads two ways, naming both readings", () => {
    const problem = coerceReferencePath("p/target", "foo.bar/baz/qux")
    expect(problem.value).toBeUndefined()
    expect(problem.error).toContain("ambiguous")
    // Both ends, so either can be the one to change.
    expect(problem.error).toContain("foo.bar/baz")
    expect(problem.error).toContain("p/target/foo.bar/baz/qux")
  })

  it("refuses an empty-segment shape, which names no record", () => {
    // Neither parses as a path, so each is read as a short form, and a short
    // form with a slash on an end is an id of nothing.
    for (const bad of ["target/", "/x"]) {
      expect(coerceReferencePath(FUNCTION, bad).value).toBeUndefined()
      expect(coerceReferencePath(FUNCTION, bad).error).toMatch(
        /has an empty segment/
      )
    }
    // A full path under its own pin whose id half is empty.
    expect(coerceReferencePath(KIND, `${KIND}//b`).error).toMatch(
      /empty id segment/
    )
    // Under any OTHER pin it is the ambiguous case, exactly as the engine
    // reads it: a pointer at the parsed kind, or an id the pin would complete.
    expect(coerceReferencePath(FUNCTION, `${KIND}//b`).error).toMatch(
      /ambiguous/
    )
  })

  it("unpinned, takes a full path and nothing else", () => {
    // Every kind carries an authority, so the qualified path is the only
    // unpinned value that names a record.
    expect(coerceReferencePath("", "foo.bar/baz/abc")).toEqual({
      value: "foo.bar/baz/abc",
    })
    // A kind identity leaves nothing for an id; a dotless first segment and a
    // bare id are no path at all.
    expect(coerceReferencePath("", "foo.bar/baz").error).toMatch(/is not one/)
    expect(coerceReferencePath("", "note/abc").error).toMatch(/is not one/)
    expect(coerceReferencePath("", "abc").error).toMatch(/is not one/)
    expect(coerceReferencePath("", "foo.bar/baz//abc").error).toMatch(
      /empty id/
    )
  })

  it("says an empty value needs an id", () => {
    expect(coerceReferencePath(FUNCTION, "").error).toBe(
      "a reference needs an id"
    )
  })
})
