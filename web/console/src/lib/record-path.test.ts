/** The console's half of `internal/vocabulary`'s record-path grammar. The cases
 * are the Go suite's (`TestSplitRecordPath`), deliberately: the two splits are
 * a mirror, and a case that passes there and fails here is the drift this file
 * exists to catch. */

import { describe, expect, it } from "vitest"

import { recordPath, splitRecordPath } from "./record-path"

describe("splitRecordPath", () => {
  it("splits a qualified kind's path on the kind grammar, not a registry", () => {
    expect(
      splitRecordPath("core.substrate.reamde.dev/llmprovider/claude")
    ).toEqual({ kind: "core.substrate.reamde.dev/llmprovider", id: "claude" })
  })

  it("reads a dotless first segment as a repository-local kind", () => {
    expect(splitRecordPath("task/abc123")).toEqual({
      kind: "task",
      id: "abc123",
    })
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
    expect(splitRecordPath("task/a/b/c")).toEqual({ kind: "task", id: "a/b/c" })
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
      "task/abc123",
      "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task",
    ]) {
      const parts = splitRecordPath(path)
      expect(parts && recordPath(parts.kind, parts.id)).toBe(path)
    }
  })
})
