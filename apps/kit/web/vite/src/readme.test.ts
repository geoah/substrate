import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { describe, expect, it } from "vitest"

// Here because this is the kit's one package that reads the tree from disk.
const kit = resolve(dirname(new URL(import.meta.url).pathname), "../..")

describe("the kit's README", () => {
  it("shows the template's files as they are", () => {
    const readme = readFileSync(resolve(kit, "README.md"), "utf8")
    const shown = [
      ...readme.matchAll(
        /\]\((\.\.\/\.\.\/template\/web\/src\/[^)]+)\):\n\n```tsx?\n([\s\S]*?)```/g
      ),
    ]
    expect(shown.length).toBeGreaterThanOrEqual(3)
    for (const [, path, code] of shown)
      expect(code, path).toBe(readFileSync(resolve(kit, path), "utf8"))
  })
})
