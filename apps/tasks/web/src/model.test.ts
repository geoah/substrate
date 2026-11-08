import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import {
  Kinds,
  addDays,
  banded,
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

function task(id: string, properties: Record<string, unknown>): TaskItem {
  const r: SubstrateRecord = {
    id,
    kind: Kinds.task,
    properties: { name: id, status: "open", ...properties },
    version: 1,
    createdAt: "2026-11-01T10:00:00Z",
    updatedAt: "2026-11-01T10:00:00Z",
  }
  return readTask(r)
}

describe("Needs you's bands", () => {
  it("bands and ranks as TaskBand does", () => {
    const tasks = [
      task("later", { dueDay: addDays(today, 20) }),
      task("undated-urgent", { priority: "urgent" }),
      task("late-1", { dueDay: addDays(today, -1) }),
      task("late-4", { dueDay: addDays(today, -4) }),
      task("tomorrow", { dueDay: addDays(today, 1) }),
      task("undated", {}),
      task("undated-high", { priority: "high" }),
      task("done", { dueDay: addDays(today, -2), status: "done" }),
    ]
    expect(
      banded(tasks, undefined, now).map((r) => `${r.band}:${r.task.id}`)
    ).toEqual([
      "needsYou:late-4",
      "needsYou:late-1",
      "needsYou:undated-urgent",
      "needsYou:undated-high",
      "upcoming:tomorrow",
      "later:later",
      "later:undated",
    ])
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
    const list = { id: "l", name: "Home", version: 1 }
    const one = [task("a", { list: `${Kinds.list}/l` })]
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
