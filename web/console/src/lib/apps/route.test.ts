/** The record segment's two history moves, run over a stand-in history: a
 * row tap pushes one entry stamped `SHEET_STATE`, closing the sheet pops
 * exactly that entry, and the chrome's back arrow then leaves the view. The
 * same moves on a segment reached by a reload replace in place, so history
 * never gains an entry the person did not make. */

import { describe, expect, it } from "vitest"

import {
  backMove,
  parseRecordSegment,
  SHEET_STATE,
  sheetClose,
  type SheetState,
} from "./route"

interface Entry {
  path: string
  state?: SheetState
}

/** Enough of `window.history` to run the sequence: a stack with an index,
 * push, replace and back. */
class History {
  entries: Entry[]
  index: number
  constructor(entries: Entry[]) {
    this.entries = entries
    this.index = entries.length - 1
  }
  get current(): Entry {
    return this.entries[this.index]
  }
  get canGoBack(): boolean {
    return this.index > 0
  }
  push(entry: Entry) {
    this.entries = [...this.entries.slice(0, this.index + 1), entry]
    this.index = this.entries.length - 1
  }
  replace(entry: Entry) {
    this.entries = [...this.entries]
    this.entries[this.index] = entry
  }
  back() {
    if (this.index > 0) this.index -= 1
  }
}

const VIEW = "/views/people-contacts"
const RECORD = `${VIEW}/ada.example.com%2Fpeople%2Fperson%2Fp1`

/** What the screen does on close, as `useScreenRecord.closeSheet` does. */
function closeSheet(h: History) {
  if (sheetClose(h.current.state) === "back") h.back()
  else h.replace({ path: VIEW })
}

/** What the chrome's back arrow does, as `useBack` does. */
function backArrow(h: History, sheetOpen: boolean) {
  switch (backMove(sheetOpen, h.canGoBack)) {
    case "close":
      closeSheet(h)
      return
    case "back":
      h.back()
      return
    default:
      h.replace({ path: "/apps" })
  }
}

describe("close, then back", () => {
  it("pops the tap's entry and then leaves the view", () => {
    const h = new History([{ path: "/apps" }, { path: VIEW }])
    h.push({ path: RECORD, state: SHEET_STATE })
    expect(h.entries).toHaveLength(3)

    closeSheet(h)
    expect(h.current.path).toBe(VIEW)
    expect(h.index).toBe(1)
    // Nothing was added: the close is the tap undone.
    expect(h.entries.slice(0, h.index + 1).map((e) => e.path)).toEqual([
      "/apps",
      VIEW,
    ])

    backArrow(h, false)
    expect(h.current.path).toBe("/apps")
  })

  it("closes an open sheet before it leaves", () => {
    const h = new History([{ path: "/apps" }, { path: VIEW }])
    h.push({ path: RECORD, state: SHEET_STATE })
    backArrow(h, true)
    expect(h.current.path).toBe(VIEW)
    backArrow(h, false)
    expect(h.current.path).toBe("/apps")
  })

  it("replaces in place after a reload, then goes to the launcher", () => {
    const h = new History([{ path: RECORD }])
    closeSheet(h)
    expect(h.current.path).toBe(VIEW)
    expect(h.entries).toHaveLength(1)
    backArrow(h, false)
    expect(h.current.path).toBe("/apps")
  })

  it("pops after a forward brought the tap's entry back", () => {
    const h = new History([{ path: VIEW }])
    h.push({ path: RECORD, state: SHEET_STATE })
    h.back()
    // forward: the entry and its state are restored by the browser
    h.index += 1
    expect(h.current.state).toEqual(SHEET_STATE)
    closeSheet(h)
    expect(h.current.path).toBe(VIEW)
    expect(h.entries).toHaveLength(2)
  })
})

describe("sheetClose and backMove", () => {
  it("reads the stamp and nothing else", () => {
    expect(sheetClose(SHEET_STATE)).toBe("back")
    expect(sheetClose({ sheet: false })).toBe("replace")
    expect(sheetClose(undefined)).toBe("replace")
  })
  it("orders close, back, launcher", () => {
    expect(backMove(true, true)).toBe("close")
    expect(backMove(true, false)).toBe("close")
    expect(backMove(false, true)).toBe("back")
    expect(backMove(false, false)).toBe("launcher")
  })
})

describe("parseRecordSegment", () => {
  it("takes the kind from a full path even when the registry lacks it", () => {
    const t = parseRecordSegment("other.example.com/x/y/z1", undefined, [])
    expect(t.kindIdentity).toBe("other.example.com/x/y")
    expect(t.kind).toBeUndefined()
    expect(t.id).toBe("z1")
  })
  it("has no kind for a bare id on a trait view", () => {
    const t = parseRecordSegment("task27", undefined, [])
    expect(t.kindIdentity).toBeUndefined()
    expect(t.id).toBe("task27")
  })
})
