/** An ink document being edited, with its undo history. Every change is one
 * of lib/ink's pure edits, so a step back is the previous document object
 * and costs nothing to keep. One eraser gesture is one step however many
 * strokes it takes, and a gesture that erases nothing is no step at all. */

import { useEffect, useReducer, useState } from "react"

import {
  withBackground,
  withNewPage,
  withStroke,
  withoutStrokes,
  type InkBackgroundType,
  type InkDocument,
  type InkStroke,
} from "@/lib/ink"
import { strokesHit } from "@/lib/ink-render"

import { ERASER_RADIUS } from "./use-ink-drawing"

interface InkHistory {
  document: InkDocument
  past: InkDocument[]
  future: InkDocument[]
  /** A gesture began and has not changed anything yet. */
  pending: boolean
}

export type InkAction =
  | { type: "gesture" }
  | { type: "stroke"; page: number; stroke: InkStroke }
  | { type: "erase"; page: number; x: number; y: number }
  | { type: "addPage"; background: InkBackgroundType }
  | { type: "background"; page: number; background: InkBackgroundType }
  | { type: "undo" }
  | { type: "redo" }
  | { type: "reset"; document: InkDocument }

/** Undo reaches back this many steps; older ones are dropped. */
const DEPTH = 200

function step(state: InkHistory, next: InkDocument): InkHistory {
  if (next === state.document) return state
  return {
    document: next,
    past: [...state.past, state.document].slice(-DEPTH),
    future: [],
    pending: false,
  }
}

export function inkHistoryReducer(
  state: InkHistory,
  action: InkAction
): InkHistory {
  switch (action.type) {
    case "gesture":
      return { ...state, pending: true }
    case "stroke":
      return step(state, withStroke(state.document, action.page, action.stroke))
    case "erase": {
      const page = state.document.pages[action.page]
      if (!page) return state
      const hit = strokesHit(page.strokes, action.x, action.y, ERASER_RADIUS)
      if (!hit.length) return state
      const next = withoutStrokes(state.document, action.page, new Set(hit))
      // The gesture's first erase opens the step; the rest join it.
      return state.pending ? step(state, next) : { ...state, document: next }
    }
    case "addPage":
      return step(state, withNewPage(state.document, action.background))
    case "background":
      return step(
        state,
        withBackground(state.document, action.page, action.background)
      )
    case "undo": {
      const previous = state.past[state.past.length - 1]
      if (!previous) return state
      return {
        document: previous,
        past: state.past.slice(0, -1),
        future: [state.document, ...state.future],
        pending: false,
      }
    }
    case "redo": {
      const next = state.future[0]
      if (!next) return state
      return {
        document: next,
        past: [...state.past, state.document],
        future: state.future.slice(1),
        pending: false,
      }
    }
    case "reset":
      return { document: action.document, past: [], future: [], pending: false }
  }
}

export function useInkHistory(initial: InkDocument) {
  // The document the editor opened on, held: a caller that rebuilds its
  // `initial` on every render must not make an untouched page read as
  // changed.
  const [opened] = useState(initial)
  const [state, dispatch] = useReducer(inkHistoryReducer, {
    document: opened,
    past: [],
    future: [],
    pending: false,
  })
  return {
    document: state.document,
    canUndo: state.past.length > 0,
    canRedo: state.future.length > 0,
    /** Whether anything changed since the editor opened. */
    dirty: state.document !== opened,
    dispatch,
  }
}

/** Ctrl/Cmd+Z and Shift+Ctrl/Cmd+Z, while the editor is open. */
export function useUndoKeys(dispatch: (action: InkAction) => void) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "z")
        return
      const target = event.target as HTMLElement | null
      if (target?.closest("input, textarea, [contenteditable=true]")) return
      event.preventDefault()
      dispatch({ type: event.shiftKey ? "redo" : "undo" })
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [dispatch])
}
