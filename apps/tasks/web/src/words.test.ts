import { describe, expect, it } from "vitest"
import { kindLabel } from "./sources"
import { words } from "./words"

// The contract's Words table (apps/tasks/contract/README.md), held here as
// it is written there, so a change on either side is a change on both.
describe("the contract's words", () => {
  it("says Needs you's two rows", () => {
    expect(words.queueRow.title(6)).toBe("6 suggestions")
    expect(words.queueRow.title(1)).toBe("1 suggestion")
    expect(words.queueRow.detail).toBe(
      "Tasks waiting for you to accept or dismiss"
    )
    expect(words.noDateRow).toEqual({
      title: "No date",
      detail: "Open tasks with no due date",
    })
  })

  it("says the queue, a suggestion's row and its page", () => {
    expect(words.queue).toEqual({
      title: "Suggestions",
      detail: "Accept one to put it on your list, or dismiss it.",
      select: "Select",
      emptyTitle: "No suggestions",
      emptyMessage:
        "Suggested tasks show here until you accept or dismiss them.",
    })
    expect(words.suggestionRow.meta("Chores", "2h ago")).toBe("Chores · 2h ago")
    expect([words.suggestionRow.dismiss, words.suggestionRow.accept]).toEqual([
      "Dismiss",
      "Accept",
    ])
    expect(words.suggestion.heading("Chores", "2 hours ago")).toBe(
      "Suggestion in Chores · written 2 hours ago"
    )
    expect([
      words.suggestion.details,
      words.suggestion.collection,
      words.suggestion.dismiss,
      words.suggestion.accept,
    ]).toEqual(["Details", "Collection", "Dismiss", "Accept"])
  })

  it("says Select", () => {
    expect(words.select.select).toBe("Select")
    expect(words.select.picked(2)).toBe("2 selected")
    expect([
      words.select.accept,
      words.select.dismiss,
      words.select.cancel,
    ]).toEqual(["Accept", "Dismiss", "Cancel"])
  })

  it("says the chooser and the undo", () => {
    expect(words.chooser).toEqual({
      accept: "Accept as",
      dismiss: "Dismiss as",
      complete: "Done as",
      wontDo: "Won't do as",
      reopen: "Reopen as",
      cancel: "Cancel",
    })
    expect(words.undo).toEqual({
      accept: "Accepted",
      dismiss: "Dismissed",
      complete: "Done",
      wontDo: "Won't do",
      action: "Undo",
    })
  })

  it("says Lists", () => {
    expect(words.lists.search(["Lists", "Projects"])).toBe(
      "Search lists and projects"
    )
    expect(words.lists.pinned).toBe("Pinned")
    expect(words.lists.none("List")).toBe("No list")
    expect(words.lists.none("Project")).toBe("No project")
    expect(words.lists.noneDetail("Project")).toBe("Tasks with no project")
    expect(words.lists.noneDetail("List")).toBe("Tasks with no list")
    expect(words.lists.missing).toBe("Missing")
    expect(words.lists.missingDetail("Project")).toBe(
      "Tasks whose project is gone or out of reach"
    )
    expect(words.lists.more(89)).toBe("89 more with nothing open")
    expect(words.lists.ungrouped).toBe("Collections")
  })

  it("says a group", () => {
    expect(words.group.kind("Project", "Active")).toBe("Project · Active")
    expect(words.group.open(3)).toBe("3 open")
    expect(words.group.outOfReach).toBe("Out of reach")
    expect(words.viewOnly).toBe("View only")
    expect([
      words.group.todo,
      words.group.done,
      words.group.pin,
      words.group.unpin,
    ]).toEqual(["To Do", "Done", "Pin", "Unpin"])
  })

  it("says Settings, Collections and a collection", () => {
    expect(words.settings.collections).toBe("Collections")
    expect(words.settings.on(2)).toBe("2 on")
    expect(words.collections.title).toBe("Collections")
    expect(words.collections.detail).toBe(
      "Every collection of yours with a done state. Turn one on to see its tasks here; Substrate Tasks then asks to change them."
    )
    expect(words.collections.showFrom).toBe("Show tasks from")
    expect(words.collections.open(12)).toBe("12 open")
    expect(words.collections.suggested(6)).toBe("6 suggested")
    expect(words.collections.groupedBy("Project")).toBe("grouped by project")
    expect(words.collections.viewOnly).toBe("View only")
    expect(words.collections.footer).toBe(
      "Turning a collection off only hides it here. Its tasks stay where they are."
    )
    expect(words.collections.saveTo).toBe("Save new tasks to")
    expect(words.collections.newTasks).toBe("New tasks")
    expect(words.collection).toEqual({
      groupBy: "Group by",
      nothing: "Nothing",
      nothingDetail: "One list of every task",
      states: "How its states read",
      proposed: "Suggestion",
      open: "Open",
      done: "Done",
      dropped: "Dropped",
      fields: "Fields the app shows",
      title: "Title",
      due: "Due",
      dueDay: "Due day",
      priority: "Priority",
      order: "Order",
      none: "None",
    })
  })

  it("asks for approval", () => {
    expect(words.approval.ask("Chores")).toBe(
      "Substrate Tasks needs your approval to change your Chores."
    )
    expect(words.approval.approve).toBe("Approve")
    expect(words.approval.refused("Chores")).toBe(
      "Not approved. Chores stays view only."
    )
    expect(words.approval.readAsk("Areas")).toBe(
      "Substrate Tasks needs your approval to read your Areas."
    )
    expect(words.approval.readRefused("Chores")).toBe(
      "Not approved. Chores stays ungrouped."
    )
  })

  it("labels a group kind whose declaration the token cannot read by its name", () => {
    expect(kindLabel("ada.example.com/work/area")).toEqual({
      singular: "Area",
      plural: "Areas",
    })
    expect(
      kindLabel("ada.example.com/work/area", {
        identity: "ada.example.com/work/area",
        label: { singular: "Work area", plural: "Work areas" },
        definition: {},
      })
    ).toEqual({ singular: "Work area", plural: "Work areas" })
  })
})
