/** The words of the screens over every collection, as the contract writes
 * them once for every platform (apps/tasks/contract/README.md, Words):
 * TasksPresenter says the same. A count of one takes the singular. */

const count = (n: number, one: string, many: string) =>
  n === 1 ? `1 ${one}` : `${n} ${many}`

export const words = {
  queueRow: {
    title: (n: number) => count(n, "suggestion", "suggestions"),
    detail: "Tasks waiting for you to accept or dismiss",
  },
  noDateRow: {
    title: "No date",
    detail: "Open tasks with no due date",
  },
  queue: {
    title: "Suggestions",
    detail: "Accept one to put it on your list, or dismiss it.",
    select: "Select",
    emptyTitle: "No suggestions",
    emptyMessage: "Suggested tasks show here until you accept or dismiss them.",
  },
  suggestionRow: {
    meta: (collection: string, written: string) => `${collection} · ${written}`,
    dismiss: "Dismiss",
    accept: "Accept",
  },
  suggestion: {
    heading: (collection: string, written: string) =>
      `Suggestion in ${collection} · written ${written}`,
    details: "Details",
    collection: "Collection",
    dismiss: "Dismiss",
    accept: "Accept",
  },
  chooser: {
    accept: "Accept as",
    dismiss: "Dismiss as",
    complete: "Done as",
    wontDo: "Won't do as",
    reopen: "Reopen as",
    cancel: "Cancel",
  },
  select: {
    select: "Select",
    picked: (n: number) => `${n} selected`,
    accept: "Accept",
    dismiss: "Dismiss",
    cancel: "Cancel",
  },
  undo: {
    accept: "Accepted",
    dismiss: "Dismissed",
    complete: "Done",
    wontDo: "Won't do",
    action: "Undo",
  },
  lists: {
    /** The group kinds' plural labels, lower case, joined. */
    search: (plurals: string[]) => `Search ${joined(plurals.map(lower))}`,
    pinned: "Pinned",
    none: (reference: string) => `No ${lower(reference)}`,
    noneDetail: (reference: string) => `Tasks with no ${lower(reference)}`,
    missing: "Missing",
    missingDetail: (reference: string) =>
      `Tasks whose ${lower(reference)} is gone or out of reach`,
    more: (n: number) => `${n} more with nothing open`,
    /** The section of the collections that are on and group by nothing. */
    ungrouped: "Collections",
  },
  group: {
    kind: (label: string, status?: string) =>
      status ? `${label} · ${status}` : label,
    open: (n: number) => `${n} open`,
    todo: "To Do",
    /** A task's group while its kind waits on the owner's approval. */
    outOfReach: "Out of reach",
    done: "Done",
    pin: "Pin",
    unpin: "Unpin",
  },
  settings: {
    collections: "Collections",
    on: (n: number) => `${n} on`,
  },
  collections: {
    title: "Collections",
    detail:
      "Every collection of yours with a done state. Turn one on to see its tasks here; Substrate Tasks then asks to change them.",
    showFrom: "Show tasks from",
    open: (n: number) => `${n} open`,
    suggested: (n: number) => `${n} suggested`,
    groupedBy: (reference: string) => `grouped by ${lower(reference)}`,
    viewOnly: "View only",
    footer:
      "Turning a collection off only hides it here. Its tasks stay where they are.",
    saveTo: "Save new tasks to",
    newTasks: "New tasks",
  },
  collection: {
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
  },
  /** A view-only collection's suggestion row, in place of its decisions. */
  viewOnly: "View only",
  approval: {
    ask: (collection: string) =>
      `Substrate Tasks needs your approval to change your ${collection}.`,
    approve: "Approve",
    refused: (collection: string) =>
      `Not approved. ${collection} stays view only.`,
    readAsk: (groupKindPlural: string) =>
      `Substrate Tasks needs your approval to read your ${groupKindPlural}.`,
    readRefused: (collection: string) =>
      `Not approved. ${collection} stays ungrouped.`,
  },
} as const

function lower(text: string): string {
  return text.toLowerCase()
}

/** "lists", "lists and projects", "lists, projects and areas". */
function joined(parts: string[]): string {
  if (parts.length <= 1) return parts[0] ?? ""
  return `${parts.slice(0, -1).join(", ")} and ${parts.at(-1)}`
}
