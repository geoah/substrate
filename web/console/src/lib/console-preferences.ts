/** The console's per-repository preferences: navigation (collapsed groups,
 * favorites) and display (layout widths, density, technical details, theme).
 * They live on ONE record,
 * `substrate.reamde.dev/core/consolepreference/navigation`, so every session
 * of the repository shares them.
 *
 * Whether the sidebar is open is the exception: it is a fact about one window
 * (a laptop tucks it away, a desktop monitor keeps it), so it lives in this
 * browser's localStorage only and is never written to the record, and a
 * value an older console stored there is ignored.
 *
 * A repository whose stored `consolepreference` kind predates a setting
 * REFUSES the undeclared property, so a setting is written to the record only
 * when the stored declaration names it and is otherwise kept in this browser's
 * localStorage. A read takes the record first, then localStorage, then the
 * default. */

import { queryOptions } from "@tanstack/react-query"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  collectionPath,
  request,
} from "@/lib/api/http"
import { getRepository } from "@/lib/api/session"
import { putRecord } from "@/lib/api/records"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"

export type RecordWidth = "narrow" | "wide" | "full"
export type TableWidth = "wide" | "full"
export type Density = "comfortable" | "compact"
export type ThemePreference = "system" | "light" | "dark"

export interface NavigationPreferences {
  collapsed: string[]
  favorites: string[]
  sidebarOpen: boolean
}

export interface DisplaySettings {
  recordWidth: RecordWidth
  tableWidth: TableWidth
  density: Density
  technicalDetails: boolean
  theme: ThemePreference
}

export type ConsolePreferences = NavigationPreferences & DisplaySettings

export type SettingKey = keyof DisplaySettings

export type NavigationAction =
  | { type: "collapse"; key: string; collapsed: boolean }
  | { type: "favorite"; key: string; starred: boolean }
  | { type: "move"; key: string; direction: -1 | 1 }
  | { type: "sidebar"; open: boolean }

export type SettingAction = {
  [K in SettingKey]: { type: "set"; key: K; value: DisplaySettings[K] }
}[SettingKey]

export type ConsoleAction = NavigationAction | SettingAction

export const CONSOLE_PREFERENCE_KIND = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/consolepreference`
const NAME = "consolepreference"
const ID = "navigation"

/** Declared by every version of the kind, so always written. */
const NAVIGATION_KEYS = ["collapsed", "favorites"] as const

export const DEFAULT_SETTINGS: DisplaySettings = {
  recordWidth: "wide",
  tableWidth: "full",
  density: "comfortable",
  technicalDetails: false,
  theme: "system",
}

export const SETTING_KEYS = Object.keys(DEFAULT_SETTINGS) as SettingKey[]

const ALLOWED: { [K in SettingKey]: readonly DisplaySettings[K][] } = {
  recordWidth: ["narrow", "wide", "full"],
  tableWidth: ["wide", "full"],
  density: ["comfortable", "compact"],
  technicalDetails: [true, false],
  theme: ["system", "light", "dark"],
}

function valid<K extends SettingKey>(
  key: K,
  value: unknown
): value is DisplaySettings[K] {
  return (ALLOWED[key] as readonly unknown[]).includes(value)
}

// ── this browser's fallback ─────────────────────────────────────────────────

/** `theme` keeps the key the theme provider has always used, so a browser
 * that chose one before the setting reached the record keeps it. */
export function localKey(key: SettingKey): string {
  return key === "theme" ? "theme" : `substrate.console.${key}`
}

function storage(): Storage | undefined {
  try {
    return globalThis.localStorage ?? undefined
  } catch {
    return undefined
  }
}

function decode(raw: string | null): unknown {
  if (raw === null) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    // The theme provider stores its value bare.
    return raw
  }
}

/** What this browser keeps: every display setting it has seen, and the
 * sidebar, which only it keeps. */
export type LocalPreferences = Partial<DisplaySettings> & {
  sidebarOpen?: boolean
}

const SIDEBAR_KEY = "substrate.console.sidebarOpen"

export function readLocalSettings(): LocalPreferences {
  const store = storage()
  const out: Partial<Record<SettingKey | "sidebarOpen", unknown>> = {}
  if (!store) return out as LocalPreferences
  for (const key of SETTING_KEYS) {
    let value: unknown
    try {
      value = decode(store.getItem(localKey(key)))
    } catch {
      continue
    }
    if (valid(key, value)) out[key] = value
  }
  try {
    const open = decode(store.getItem(SIDEBAR_KEY))
    if (typeof open === "boolean") out.sidebarOpen = open
  } catch {
    // An unreadable store opens the sidebar.
  }
  return out as LocalPreferences
}

export function writeLocalSidebar(open: boolean): void {
  try {
    storage()?.setItem(SIDEBAR_KEY, JSON.stringify(open))
  } catch {
    // A full or refused storage loses a convenience, never the page.
  }
}

export function writeLocalSetting(action: SettingAction): void {
  const store = storage()
  if (!store) return
  try {
    store.setItem(
      localKey(action.key),
      typeof action.value === "string"
        ? action.value
        : JSON.stringify(action.value)
    )
  } catch {
    // A full or refused storage loses a convenience, never the page.
  }
}

// ── the record ──────────────────────────────────────────────────────────────

const strings = (value: unknown): string[] =>
  Array.isArray(value)
    ? [...new Set(value.filter((v): v is string => typeof v === "string"))]
    : []

/** The record's preferences, each display setting falling back to this
 * browser's, then to the default. */
export function preferencesOf(
  record?: SubstrateRecord | null,
  local: LocalPreferences = {}
): ConsolePreferences {
  const properties = record?.properties ?? {}
  const settings = { ...DEFAULT_SETTINGS }
  for (const key of SETTING_KEYS) {
    const stored = properties[key]
    const value = valid(key, stored) ? stored : local[key]
    if (value !== undefined)
      (settings as Record<SettingKey, unknown>)[key] = value
  }
  return {
    collapsed: strings(properties.collapsed),
    favorites: strings(properties.favorites),
    sidebarOpen: local.sidebarOpen !== false,
    ...settings,
  }
}

export function applyConsoleAction(
  prefs: ConsolePreferences,
  action: ConsoleAction
): ConsolePreferences {
  if (action.type === "set") {
    if (!valid(action.key, action.value)) return prefs
    return { ...prefs, [action.key]: action.value }
  }
  if (action.type === "sidebar") return { ...prefs, sidebarOpen: action.open }
  if (action.type === "move") {
    const favorites = [...prefs.favorites]
    const from = favorites.indexOf(action.key)
    const to = from + action.direction
    if (from >= 0 && to >= 0 && to < favorites.length) {
      ;[favorites[from], favorites[to]] = [favorites[to], favorites[from]]
    }
    return { ...prefs, favorites }
  }
  const field = action.type === "collapse" ? "collapsed" : "favorites"
  const include = action.type === "collapse" ? action.collapsed : action.starred
  if (prefs[field].includes(action.key) === include) return prefs
  const values = prefs[field].filter((key) => key !== action.key)
  if (include) values.push(action.key)
  return { ...prefs, [field]: values }
}

/** The display settings the STORED preference kind declares: only those may
 * be written to the record. */
export function declaredSettings(kinds: readonly KindInfo[]): Set<SettingKey> {
  const kind = kinds.find((k) => k.identity === CONSOLE_PREFERENCE_KIND)
  const properties = kind?.definition?.properties
  const out = new Set<SettingKey>()
  if (!properties || typeof properties !== "object") return out
  for (const key of SETTING_KEYS) if (key in properties) out.add(key)
  return out
}

async function readPreferences(
  signal?: AbortSignal
): Promise<SubstrateRecord | null> {
  try {
    return await request<SubstrateRecord>(
      "GET",
      `${collectionPath(CORE_AUTHORITY, CORE_PACKAGE_NAME, NAME)}/${ID}`,
      undefined,
      { signal }
    )
  } catch (error) {
    if (error instanceof ApiError && error.code === "not_found") return null
    throw error
  }
}

export function consolePreferencesOptions() {
  return queryOptions({
    queryKey: ["console-preferences", getRepository()],
    queryFn: ({ signal }) => readPreferences(signal),
    staleTime: 30_000,
  })
}

/** Apply one action where it lives. The sidebar, and a setting the stored
 * kind does not declare, stay in this browser and answer null; everything
 * else is a
 * read-modify-CAS on the record, which keeps another session's unrelated
 * edits, retried on a conflict against fresh state. A written setting is
 * mirrored into localStorage too, so a browser that has not read the record
 * yet (the sign-in page) still starts from it. */
export async function saveConsoleAction(
  action: ConsoleAction,
  declared: ReadonlySet<SettingKey> = new Set()
): Promise<SubstrateRecord | null> {
  if (action.type === "sidebar") {
    writeLocalSidebar(action.open)
    return null
  }
  if (action.type === "set") {
    writeLocalSetting(action)
    if (!declared.has(action.key)) return null
  }
  const keys: string[] = [
    ...NAVIGATION_KEYS,
    ...SETTING_KEYS.filter((k) => declared.has(k)),
  ]
  for (let attempt = 0; attempt < 3; attempt++) {
    const record = await readPreferences()
    const next = applyConsoleAction(
      preferencesOf(record, readLocalSettings()),
      action
    )
    const properties: Record<string, unknown> = {}
    for (const key of keys) properties[key] = next[key as keyof typeof next]
    try {
      return await putRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, NAME, ID, {
        properties,
        ifVersion: record?.version ?? 0,
      })
    } catch (error) {
      if (
        !(error instanceof ApiError && error.code === "conflict") ||
        attempt === 2
      )
        throw error
    }
  }
  throw new Error("Console preferences could not be saved")
}
