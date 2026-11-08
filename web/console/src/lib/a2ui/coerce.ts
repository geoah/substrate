/** How a resolved A2UI value reads as the type a prop wants. */

export function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

/** A value as text: nothing is the empty string, a structure its JSON. */
export function toText(value: unknown): string {
  if (value === undefined || value === null) return ""
  if (typeof value === "string") return value
  if (typeof value === "number" || typeof value === "boolean")
    return String(value)
  try {
    return JSON.stringify(value)
  } catch {
    return ""
  }
}

/** A DynamicBoolean: a validation result counts by its `valid`. */
export function toBool(value: unknown): boolean {
  if (isRecord(value) && typeof value.valid === "boolean") return value.valid
  return value === true
}

/** A number, or undefined for anything that is not one (`""` included). */
export function toNumber(value: unknown): number | undefined {
  if (typeof value === "number")
    return Number.isFinite(value) ? value : undefined
  if (typeof value === "string" && value.trim() !== "") {
    const n = Number(value)
    return Number.isFinite(n) ? n : undefined
  }
  return undefined
}

/** A DynamicStringList: an array's strings; one string is a list of one. */
export function toStringList(value: unknown): string[] {
  if (Array.isArray(value))
    return value.filter((v): v is string => typeof v === "string")
  if (typeof value === "string" && value) return [value]
  return []
}
