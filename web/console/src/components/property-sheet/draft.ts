/** A property sheet over a record that does not exist yet. Inside a
 * `SheetDraftContext` every in-place write (the row editors, a bool's toggle,
 * the body) lands in the draft through `write` instead of a PATCH, and the
 * sheet asks the draft which rows to show, which to fold, and what is wrong
 * with them. The editors are the record page's own, so a new record is edited
 * exactly as it will be once it exists. */

import { createContext, useContext } from "react"

import type { SheetRow } from "./sheet-rows"

export interface SheetDraft {
  /** One write, in the shape a PATCH would carry: a property per key, and
   * `null` to take one out. */
  write: (properties: Record<string, unknown>) => void
  /** The rows to show and the rows behind the "N more" line, each in the
   * order it reads. Rows left out of both are not offered. */
  arrange: (rows: SheetRow[]) => { shown: SheetRow[]; folded: SheetRow[] }
  /** What is wrong with a row, by property name, in the sheet's own error
   * style. The draft decides when a row has been asked about. */
  errors: Record<string, string>
}

/** Wrap a sheet (and the body beside it) in this to edit a draft. */
export const SheetDraftContext = createContext<SheetDraft | undefined>(
  undefined
)

/** The draft the sheet is editing, or undefined on a stored record. */
export function useSheetDraft(): SheetDraft | undefined {
  return useContext(SheetDraftContext)
}
