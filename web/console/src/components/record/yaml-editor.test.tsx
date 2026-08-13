/** The YAML lens's wiring: the editor holds the page's document, an edit made
 * in it is reported out, and a change made ELSEWHERE (the form lens, a reseed)
 * is dispatched in without rebuilding the editor under the cursor. */

import { EditorView } from "@codemirror/view"
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { YamlEditor } from "./yaml-editor"

const taskKind: KindInfo = {
  identity: "tasks.substrate.geoah.me/task",
  name: "task",
  authority: "tasks.substrate.geoah.me",
  version: "",
  plural: "tasks",
  source: "installed",
  definition: {
    properties: { title: { type: "string", required: true } },
  },
}

const DOC = "kind: tasks.substrate.geoah.me/task\ndata:\n  properties:\n    title: hi\n"

function editor() {
  const view = EditorView.findFromDOM(
    screen.getByLabelText("Record YAML") as HTMLElement
  )
  if (!view) throw new Error("the editor is not mounted")
  return view
}

afterEach(cleanup)

describe("the YAML lens", () => {
  it("holds the document it was given", () => {
    render(<YamlEditor value={DOC} onChange={vi.fn()} kind={taskKind} />)
    expect(editor().state.doc.toString()).toBe(DOC)
  })

  it("reports an edit made in it", () => {
    const onChange = vi.fn()
    render(<YamlEditor value={DOC} onChange={onChange} kind={taskKind} />)
    const view = editor()
    act(() => {
      view.dispatch({ changes: { from: view.state.doc.length, insert: "    x: 1\n" } })
    })
    expect(onChange).toHaveBeenCalledWith(expect.stringContaining("x: 1"))
  })

  it("takes a change made elsewhere without losing the editor", () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <YamlEditor value={DOC} onChange={onChange} kind={taskKind} />
    )
    const before = editor()
    const next = DOC.replace("title: hi", "title: renamed")
    rerender(<YamlEditor value={next} onChange={onChange} kind={taskKind} />)
    expect(editor().state.doc.toString()).toBe(next)
    // The same editor instance: a rebuild would drop the cursor and the undo
    // history mid-edit.
    expect(editor()).toBe(before)
  })
})
