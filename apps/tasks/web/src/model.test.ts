import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import {
  List,
  Task,
  addDays,
  civilDay,
  dueWords,
  lateWords,
  notesPreview,
  readTask,
  type TaskItem,
} from "./model"
import { deleteListQuestion } from "./present"

const now = new Date(2026, 10, 8, 12, 0)
const today = civilDay(now)

function task(
  id: string,
  properties: Record<string, unknown>
): Omit<TaskItem, "item"> {
  const r: SubstrateRecord = {
    id,
    kind: Task.kind,
    properties: { name: id, status: "open", ...properties },
    version: 1,
    createdAt: "2026-11-01T10:00:00Z",
    updatedAt: "2026-11-01T10:00:00Z",
  }
  return readTask(r)
}

describe("the app's own task", () => {
  it("is named by its record path, and its list by the list's", () => {
    const t = task("a", { list: { ref: List.ref("l") } })
    expect(t.id).toBe(`${Task.kind}/a`)
    expect(t.recordID).toBe("a")
    expect(t.list).toBe(List.ref("l"))
  })
})

describe("words", () => {
  it("says a due date as a row does", () => {
    expect(dueWords({ day: today }, now)).toEqual({
      text: "Today",
      tone: "soon",
    })
    expect(dueWords({ day: addDays(today, 1) }, now).text).toBe("Tomorrow")
    expect(dueWords({ day: addDays(today, -1) }, now)).toEqual({
      text: "Overdue: Yesterday",
      tone: "overdue",
    })
  })

  it("says how late a task is", () => {
    expect(lateWords({ day: addDays(today, -1) }, now)).toBe("1 day late")
    expect(lateWords({ day: addDays(today, -4) }, now)).toBe("4 days late")
    const at = new Date(2026, 10, 8, 9, 0)
    expect(lateWords({ day: today, at }, now)).toMatch(/^Late since /)
  })

  it("puts notes on one line without their marks", () => {
    expect(
      notesPreview(
        "Ask for **Thursday**\n\n- [ ] call [them](https://x.example)"
      )
    ).toBe("Ask for Thursday call them")
    expect(notesPreview("x".repeat(200))).toHaveLength(160)
  })

  it("asks before a list goes, naming its tasks", () => {
    const list = { id: List.ref("l"), name: "Home", version: 1 }
    const one = [task("a", { list: List.ref("l") }) as TaskItem]
    expect(deleteListQuestion(list, [])).toMatchObject({
      title: "Delete “Home”?",
      message: "The list has no tasks.",
      confirm: "Delete List",
    })
    expect(deleteListQuestion(list, one)).toMatchObject({
      message: "Its 1 task is deleted with it.",
      confirm: "Delete List and 1 Task",
    })
    expect(deleteListQuestion({ ...list, name: "" }, []).title).toBe(
      "Delete this list?"
    )
  })
})
