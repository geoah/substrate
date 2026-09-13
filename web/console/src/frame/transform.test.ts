/** The transform: TSX in, an ES module out with the same number of lines
 * (so a runtime error's line is the author's), a syntax error reported with
 * its line and column, a used `#name` import kept for the import map to
 * resolve, and a type-only import elided. */

import { describe, expect, it } from "vitest"

import { transformModule } from "./transform"

const SOURCE = `import { useRecords, records } from "substrate/app"
import { Screen, Row } from "substrate/ui"
import type { Task } from "#types"
import { nameOf } from "#format"

const TASK = "ada.example.com/tasks/task"

export default function Tasks() {
  const open = useRecords({ kind: TASK, orderBy: "dueAt:asc" })
  const done = (t: Task) =>
    records.transition(TASK, t.id, "status", "done", { ifVersion: t.version })
  return (
    <Screen title="Tasks">
      {open.records.map((t) => (
        <Row key={t.id} title={nameOf(t)} onTap={() => done(t)} />
      ))}
    </Screen>
  )
}
`

/** The projects example from `.dev/seed/apps.yaml`, the budget's measure. */
const PROJECTS = `import { useRecords, useRecord, useRoute, records } from "substrate/app"
import { Screen, QuickAdd, List, Row, Check, StateBadge } from "substrate/ui"

const PROJECT = "ada.example.com/tasks/project"
const TASK = "ada.example.com/tasks/task"

function Projects() {
  const { navigate } = useRoute()
  const page = useRecords({ kind: PROJECT, orderBy: "name:asc",
    filter: { properties: { status: { in: ["active", "onhold"] } } } })
  return (
    <Screen title="Projects">
      <List page={page}>
        {(p) => <Row key={p.id} title={p.properties.name} chevron onTap={() => navigate(\`/\${p.id}\`)}
          trailing={<StateBadge value={p.properties.status} />} />}
      </List>
    </Screen>
  )
}

function Project({ id }) {
  const { record: project } = useRecord(PROJECT, id)
  const ref = \`\${PROJECT}/\${id}\`
  const tasks = useRecords({ kind: TASK, orderBy: "dueAt:asc",
    filter: { properties: { project: { eq: ref }, status: { eq: "open" } } } })
  const done = (t) => records.transition(TASK, t.id, "status", "done", { ifVersion: t.version })
  return (
    <Screen title={project?.properties.name ?? "…"} back>
      <QuickAdd kind={TASK} property="name" defaults={{ project: ref }} placeholder="Add a task" />
      <List page={tasks} empty="No open tasks">
        {(t) => <Row key={t.id} title={t.properties.name} when={t.properties.dueAt} record={t}
          leading={<Check onChange={() => done(t)} />} />}
      </List>
    </Screen>
  )
}

export default function App() {
  const { path } = useRoute()
  const id = path.slice(1)
  return id ? <Project id={id} /> : <Projects />
}
`

describe("transformModule", () => {
  it("turns TSX into an ES module with the automatic runtime and keeps every line", () => {
    const out = transformModule("source", SOURCE)
    if ("error" in out) throw new Error(out.error.message)
    expect(out.js.split("\n").length).toBe(SOURCE.split("\n").length)
    expect(out.js).toMatch(/from "react\/jsx-runtime"/)
    expect(out.js).toMatch(/_jsx\(Screen, /)
    // The type annotation is gone, the module keeps ESM syntax.
    expect(out.js).not.toMatch(/: Task\)/)
    expect(out.js).toMatch(/^export default function Tasks/m)
  })

  it("keeps a used #name import and elides a type-only one", () => {
    const out = transformModule("source", SOURCE)
    if ("error" in out) throw new Error(out.error.message)
    expect(out.js).toContain('import { nameOf } from "#format"')
    expect(out.js).not.toContain("#types")
  })

  it("reports an unclosed tag with the author's line and column", () => {
    const broken = `export default function A() {\n  return (\n    <Row title=\n  )\n}\n`
    const out = transformModule("source", broken)
    expect("error" in out).toBe(true)
    if (!("error" in out)) return
    expect(out.error).toMatchObject({ module: "source", line: 4, column: 3 })
    expect(out.error.message).toBe("Unexpected token")
  })

  it("names the module the error is in", () => {
    const out = transformModule("helper", "export const x = (")
    if (!("error" in out)) throw new Error("expected an error")
    expect(out.error.module).toBe("helper")
    expect(out.error.line).toBe(1)
  })

  it("transforms the projects example within the budget", () => {
    // Warm once: the first call pays for module initialization.
    transformModule("source", PROJECTS)
    const start = performance.now()
    const out = transformModule("source", PROJECTS)
    const ms = performance.now() - start
    expect("js" in out).toBe(true)
    // The plan expects single-digit milliseconds on a laptop; 50 ms is the
    // phone budget it names.
    expect(ms).toBeLessThan(50)
  })
})
