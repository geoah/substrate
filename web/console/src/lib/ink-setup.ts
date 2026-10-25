/** What a new note starts on, remembered in this browser: the page size and
 * paper the last note was saved with, and whether a finger draws. These are
 * facts about the device, not the person: an iPad and a laptop want
 * different sizes, and only a device with a pen wants a finger to scroll. */

import {
  INK_BACKGROUNDS,
  INK_PAGE_SIZES,
  oriented,
  type InkBackgroundType,
  type InkSize,
} from "@/lib/ink"

const KEY = "substrate.ink.setup"

export interface InkSetup {
  size: InkSize
  background: InkBackgroundType
  /** A finger draws, as a pen does. Off, it scrolls the page, and only a
   * pen or a mouse draws: what an iPad with a Pencil wants. */
  fingerDraws: boolean
}

export const DEFAULT_INK_SETUP: InkSetup = {
  size: oriented(INK_PAGE_SIZES.a4, "portrait"),
  background: "lined",
  fingerDraws: false,
}

function isSize(value: unknown): value is InkSize {
  if (typeof value !== "object" || value === null) return false
  const { width, height } = value as Record<string, unknown>
  return (
    typeof width === "number" &&
    typeof height === "number" &&
    Number.isFinite(width) &&
    Number.isFinite(height) &&
    width > 0 &&
    height > 0
  )
}

export function readInkSetup(storage: Storage | undefined = globalStorage()) {
  let stored: Record<string, unknown> = {}
  try {
    const raw = storage?.getItem(KEY)
    if (raw) stored = JSON.parse(raw) as Record<string, unknown>
  } catch {
    // A setup this browser cannot read is the default one.
  }
  return {
    size: isSize(stored.size) ? stored.size : DEFAULT_INK_SETUP.size,
    background: (INK_BACKGROUNDS as readonly unknown[]).includes(
      stored.background
    )
      ? (stored.background as InkBackgroundType)
      : DEFAULT_INK_SETUP.background,
    fingerDraws: stored.fingerDraws === true,
  } satisfies InkSetup
}

export function writeInkSetup(
  change: Partial<InkSetup>,
  storage: Storage | undefined = globalStorage()
): InkSetup {
  const next = { ...readInkSetup(storage), ...change }
  try {
    storage?.setItem(KEY, JSON.stringify(next))
  } catch {
    // A browser that keeps nothing starts every note on the default.
  }
  return next
}

function globalStorage(): Storage | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage
  } catch {
    return undefined
  }
}
