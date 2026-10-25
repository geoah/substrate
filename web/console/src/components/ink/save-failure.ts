/** What a refused ink save says. A conflict (another save landed after
 * editing began) keeps the editor open on the drawing and offers it as a
 * download, rather than send the reader to reload, which would lose it. */

import { writeError } from "@/components/property-sheet/use-record-patch"
import { ApiError } from "@/lib/api/types"
import type { InkDocument } from "@/lib/ink"

/** A refused save: what it says, and, when another save landed since
 * editing began, the drawing that was not stored, so it can be kept. */
export interface InkSaveFailure {
  message: string
  draft?: InkDocument
}

/** What a refused save of `draft` says. A conflict keeps the editor open on
 * the draft rather than send the reader to reload, which would lose it. */
export function inkSaveFailure(
  error: unknown,
  draft: InkDocument | undefined
): InkSaveFailure {
  if (error instanceof ApiError && error.code === "conflict")
    return {
      message:
        "This ink was not saved: another save changed this ink after you started drawing. Your drawing is still open. Download it to keep it, then cancel to see the saved version.",
      draft,
    }
  if (error instanceof ApiError && error.status === 413)
    return {
      message:
        "This note is larger than the 64 MiB a substrate stores in one file.",
    }
  return { message: writeError(error) }
}

/** What a refused import says. No editor is open, so there is no drawing to
 * keep: the file is still where it was picked from. */
export function importFailure(error: unknown): InkSaveFailure {
  if (error instanceof ApiError && error.code === "conflict")
    return {
      message:
        "The import was not saved: another save changed this note's ink since the page loaded. Reload the page and import again.",
    }
  return { message: inkSaveFailure(error, undefined).message }
}
