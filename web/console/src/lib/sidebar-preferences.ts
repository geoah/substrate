import { queryOptions } from "@tanstack/react-query"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  collectionPath,
  request,
} from "@/lib/api/http"
import { getRepository } from "@/lib/api/session"
import { putRecord } from "@/lib/api/records"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"

export interface SidebarPreferences {
  collapsed: string[]
  favorites: string[]
  sidebarOpen: boolean
}

export type SidebarAction =
  | { type: "collapse"; key: string; collapsed: boolean }
  | { type: "favorite"; key: string; starred: boolean }
  | { type: "move"; key: string; direction: -1 | 1 }
  | { type: "sidebar"; open: boolean }

const NAME = "consolepreference"
const ID = "navigation"
const strings = (value: unknown): string[] =>
  Array.isArray(value)
    ? [...new Set(value.filter((v): v is string => typeof v === "string"))]
    : []

export function preferencesOf(
  record?: SubstrateRecord | null
): SidebarPreferences {
  return {
    collapsed: strings(record?.properties.collapsed),
    favorites: strings(record?.properties.favorites),
    sidebarOpen: record?.properties.sidebarOpen !== false,
  }
}

export function applySidebarAction(
  prefs: SidebarPreferences,
  action: SidebarAction
): SidebarPreferences {
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

export function sidebarPreferencesOptions() {
  return queryOptions({
    queryKey: ["sidebar-preferences", getRepository()],
    queryFn: ({ signal }) => readPreferences(signal),
    staleTime: 30_000,
  })
}

/** Read-modify-CAS keeps another session's unrelated navigation edits. */
export async function saveSidebarAction(
  action: SidebarAction
): Promise<SubstrateRecord> {
  for (let attempt = 0; attempt < 3; attempt++) {
    const record = await readPreferences()
    const next = applySidebarAction(preferencesOf(record), action)
    try {
      return await putRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, NAME, ID, {
        properties: { ...next },
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
  throw new Error("Navigation preferences could not be saved")
}
