import { describe, expect, it } from "vitest"
import {
  actionNames,
  command,
  readSources,
  type ActionName,
  type Declaration,
  type Envelope,
} from "./sources"
import { ownDeclarations } from "./package"

/** Every case in apps/tasks/contract, run as TasksKit's tests run them: the
 * model built from the case's input, compared with what it expects and
 * with each command's writes. */

interface Case {
  name: string
  now: string
  timeZone: string
  grant: string[]
  declarations: Declaration[]
  records: Envelope[]
  expect: {
    collections: unknown[]
    items: Record<string, unknown>
    needsYou: unknown
    suggestions: string[]
    groups: unknown[]
    pins?: string[]
  }
  commands: {
    do: Partial<Record<ActionName, string>> & {
      to?: string
      edits?: Record<string, unknown>
    }
    writes: unknown[]
    choices?: string[]
  }[]
}

const cases = Object.entries(
  import.meta.glob<Case>("../../contract/*.json", {
    eager: true,
    import: "default",
  })
).map(([file, c]) => [file.split("/").at(-1)!, c] as const)

describe("the Tasks contract", () => {
  it("has its cases", () => {
    expect(cases.length).toBeGreaterThanOrEqual(4)
  })

  describe.each(cases)("%s", (_, c) => {
    const model = () =>
      readSources({
        declarations: c.declarations,
        records: c.records,
        grant: c.grant,
        now: new Date(c.now),
        timeZone: c.timeZone,
      })

    it(`reads its collections: ${c.name}`, () => {
      expect(
        model().collections.map((x) => ({
          kind: x.kind,
          enabled: x.enabled,
          viewOnly: x.viewOnly,
          creatable: x.creatable,
          title: x.title,
          due: x.due,
          dueDay: x.dueDay,
          priority: x.priority,
          order: x.order,
          group: x.group,
        }))
      ).toEqual(c.expect.collections)
    })

    it("reads every item's standing, actions, group and band", () => {
      const items = model().items
      const got: Record<string, unknown> = {}
      for (const [path, i] of items)
        got[path] = {
          standing: i.standing,
          ...i.actions,
          group: i.group,
          band: i.band,
        }
      expect(got).toEqual(c.expect.items)
    })

    it("bands Needs you and queues the suggestions", () => {
      const m = model()
      expect(m.needsYou).toEqual(c.expect.needsYou)
      expect(m.suggestions).toEqual(c.expect.suggestions)
    })

    it("sections the groups and shows the pins", () => {
      const m = model()
      expect(
        m.groups.map((g) => ({
          collection: g.collection,
          kind: g.kind,
          rows: g.rows,
        }))
      ).toEqual(c.expect.groups)
      if (c.expect.pins) expect(m.pins).toEqual(c.expect.pins)
    })

    it.each(c.commands.map((cmd, i) => [i, cmd] as const))(
      "command %i writes what it says",
      (_, cmd) => {
        const action = actionNames.find((a) => cmd.do[a] !== undefined)!
        const path = cmd.do[action]!
        const result = command(model().items.get(path), {
          action,
          path,
          to: cmd.do.to,
          edits: cmd.do.edits,
        })
        const writes =
          result.type === "write"
            ? [
                {
                  op: result.write.op,
                  path: result.write.path,
                  ifVersion: result.write.ifVersion,
                  properties: result.write.properties,
                },
              ]
            : []
        expect(writes).toEqual(cmd.writes)
        expect(result.type === "choose" ? result.choices : undefined).toEqual(
          cmd.choices
        )
      }
    )
  })
})

describe("the app's own declarations", () => {
  it("are the package's, as the contract's cases carry them", () => {
    const own = cases
      .find(([file]) => file === "own-tasks.json")![1]
      .declarations.filter((d) =>
        d.identity.startsWith("substrate.reamde.dev/tasks/")
      )
    expect(ownDeclarations).toEqual(own)
  })
})
