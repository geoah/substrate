// Display settings: what the Labs and Healthspan screens draw, kept per
// browser in localStorage under `healthspan.display`. A view preference, so
// never in the address and never per repository; the demo reads and writes
// the same key. Every read and write of the store is guarded: a private
// window, a full store or a blocked one keeps the settings for this visit
// only.

import { useCallback, useMemo, useSyncExternalStore } from "react"

export interface LabsDisplay {
  details: boolean
  trendline: boolean
  targetZones: boolean
  labRange: boolean
  sourceShapes: boolean
  key: boolean
  groupByPanel: boolean
}

export interface HealthspanDisplay {
  details: boolean
  key: boolean
  trendView: boolean
}

/** `wide` is one value for the whole app; each screen's switches live
 * under its name. `v` is the stored shape's version. */
export interface Display {
  v: 2
  /** The wide view: the card screens use the whole main area. */
  wide: boolean
  labs: LabsDisplay
  healthspan: HealthspanDisplay
}

export const DEFAULT_DISPLAY: Display = {
  v: 2,
  wide: false,
  labs: {
    details: false,
    trendline: true,
    targetZones: true,
    labRange: true,
    sourceShapes: true,
    key: true,
    groupByPanel: true,
  },
  healthspan: { details: false, key: true, trendView: true },
}

/** The localStorage key. */
export const DISPLAY_KEY = "healthspan.display"

export type DisplayPage = "labs" | "healthspan"

export type DisplayPatch = {
  wide?: boolean
  labs?: Partial<LabsDisplay>
  healthspan?: Partial<HealthspanDisplay>
}

/** What a screen hands its toolbar and cards: the settings, a patcher and
 * a reset. */
export interface DisplayControl {
  display: Display
  setDisplay: (patch: DisplayPatch) => void
  resetDisplay: () => void
}

function cleanPart<T extends object>(defaults: T, raw: unknown): T {
  const out = { ...defaults }
  if (!raw || typeof raw !== "object") return out
  for (const k of Object.keys(defaults) as (keyof T)[]) {
    const v = (raw as Record<string, unknown>)[k as string]
    if (typeof v === "boolean") (out as Record<keyof T, unknown>)[k] = v
  }
  return out
}

const obj = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" ? (v as Record<string, unknown>) : {}

/** Version 2 as stored; anything else takes the defaults. Only known keys
 * and booleans survive. */
export function cleanDisplay(raw: unknown): Display {
  const r = obj(raw)
  if (r.v === 2)
    return {
      v: 2,
      wide: typeof r.wide === "boolean" ? r.wide : DEFAULT_DISPLAY.wide,
      labs: cleanPart(DEFAULT_DISPLAY.labs, r.labs),
      healthspan: cleanPart(DEFAULT_DISPLAY.healthspan, r.healthspan),
    }
  return {
    ...DEFAULT_DISPLAY,
    labs: { ...DEFAULT_DISPLAY.labs },
    healthspan: { ...DEFAULT_DISPLAY.healthspan },
  }
}

function readStored(): string | null {
  try {
    return typeof localStorage === "undefined"
      ? null
      : localStorage.getItem(DISPLAY_KEY)
  } catch {
    return null
  }
}

/** The stored settings, cleaned; the defaults when nothing, garbage or a
 * store that cannot be read is there. */
export function loadDisplay(): Display {
  const raw = readStored()
  if (!raw) return cleanDisplay(undefined)
  try {
    return cleanDisplay(JSON.parse(raw))
  } catch {
    return cleanDisplay(undefined)
  }
}

export function saveDisplay(d: Display): void {
  try {
    if (typeof localStorage === "undefined") return
    localStorage.setItem(DISPLAY_KEY, JSON.stringify(cleanDisplay(d)))
  } catch {
    // A full or blocked store keeps the settings for this visit only.
  }
}

/** `d` with `patch` applied, cleaned. */
export function patchDisplay(d: Display, patch: DisplayPatch): Display {
  return cleanDisplay({
    v: 2,
    wide: patch.wide ?? d.wide,
    labs: { ...d.labs, ...patch.labs },
    healthspan: { ...d.healthspan, ...patch.healthspan },
  })
}

/** Whether a screen's switches or the wide view differ from their
 * defaults (the Display button's `active`). */
export function isChanged(d: Display, page: DisplayPage): boolean {
  const now = d[page] as unknown as Record<string, boolean>
  const dflt = DEFAULT_DISPLAY[page] as unknown as Record<string, boolean>
  return (
    d.wide !== DEFAULT_DISPLAY.wide ||
    Object.keys(dflt).some((k) => now[k] !== dflt[k])
  )
}

/** A screen's "Reset to defaults": its switches and the wide view. */
export function resetPatch(page: DisplayPage): DisplayPatch {
  return { wide: DEFAULT_DISPLAY.wide, [page]: DEFAULT_DISPLAY[page] }
}

/** The card grid's geometry, as `Screen width="cards"` and `CardGrid`
 * draw it: a column of at most 1088 px holding 16 px gutters either side,
 * 16 px gaps and 280 px minimum columns; in the wide view the whole main
 * area with 260 px minimum columns. Change one, change the other. */
export const CARD_COLUMN_PX = 1088
const GUTTERS_PX = 32
const GAP_PX = 16
export const CARD_MIN_PX = 280
export const CARD_MIN_WIDE_PX = 260

function columns(content: number, min: number): number {
  return Math.max(1, Math.floor((content + GAP_PX) / (min + GAP_PX)))
}

/** How many card columns a main area `width` px wide shows, in the
 * default view and in the wide view. */
export function gridColumns(width: number): { narrow: number; wide: number } {
  return {
    narrow: columns(Math.min(width, CARD_COLUMN_PX) - GUTTERS_PX, CARD_MIN_PX),
    wide: columns(width - GUTTERS_PX, CARD_MIN_WIDE_PX),
  }
}

/** Whether the wide view shows more columns than the default at `width`;
 * when it does not, its setting says "No extra room at this width". */
export function wideAddsColumns(width: number): boolean {
  const c = gridColumns(width)
  return c.wide > c.narrow
}

/** The shell's sidebar beside the main area from 768 px (App.tsx). */
export const SIDEBAR_MIN_WINDOW_PX = 768
export const SIDEBAR_PX = 240

/** The main area's width in a window `windowWidth` px wide. */
export function mainWidth(windowWidth: number): number {
  return windowWidth >= SIDEBAR_MIN_WINDOW_PX
    ? windowWidth - SIDEBAR_PX
    : windowWidth
}

// One value for the page's life, shared by every reader: the toolbar's
// switches, the cards and Settings stay in step. Another tab's write
// arrives through the storage event.
let current: Display | undefined
const listeners = new Set<() => void>()

function notify(): void {
  for (const fn of listeners) fn()
}

function onStorage(e: StorageEvent): void {
  if (e.key !== null && e.key !== DISPLAY_KEY) return
  current = loadDisplay()
  notify()
}

/** The settings as a store outside React: `get`, `set` (saves) and
 * `subscribe`. `useDisplay` reads it. */
export const displayStore = {
  get: (): Display => {
    current ??= loadDisplay()
    return current
  },
  set: (next: Display): void => {
    current = cleanDisplay(next)
    saveDisplay(current)
    notify()
  },
  subscribe: (fn: () => void): (() => void) => {
    listeners.add(fn)
    if (listeners.size === 1 && typeof window !== "undefined")
      window.addEventListener("storage", onStorage)
    return () => {
      listeners.delete(fn)
      if (!listeners.size && typeof window !== "undefined")
        window.removeEventListener("storage", onStorage)
    }
  },
}

/** The settings, a patcher and a reset, kept in step across every screen
 * that reads them. */
export function useDisplay(): [
  Display,
  (patch: DisplayPatch) => void,
  () => void,
] {
  const display = useSyncExternalStore(
    displayStore.subscribe,
    displayStore.get,
    displayStore.get
  )
  const update = useCallback((patch: DisplayPatch) => {
    displayStore.set(patchDisplay(displayStore.get(), patch))
  }, [])
  const reset = useCallback(() => {
    displayStore.set(cleanDisplay(undefined))
  }, [])
  return [display, update, reset]
}

/** `useDisplay` as the `DisplayControl` the toolbar takes. */
export function useDisplayControl(): DisplayControl {
  const [display, setDisplay, resetDisplay] = useDisplay()
  return useMemo(
    () => ({ display, setDisplay, resetDisplay }),
    [display, setDisplay, resetDisplay]
  )
}
